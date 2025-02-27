// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package sqlserver

import (
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/GuanceCloud/cliutils"
	"github.com/GuanceCloud/cliutils/logger"
	"github.com/GuanceCloud/cliutils/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
	dkio "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/obfuscate"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/tailer"
)

var (
	sample = `
[[inputs.sqlserver]]
  ## your sqlserver host ,example ip:port
  host = ""

  ## your sqlserver user,password
  user = ""
  password = ""

  ## (optional) collection interval, default is 10s
  interval = "10s"

  ## by default, support TLS 1.2 and above.
  ## set to true if server side uses TLS 1.0 or TLS 1.1
  allow_tls10 = false

  ## Set true to enable election
  election = true

  ## Database name to query. Default is master.
  database = "master"

  ## configure db_filter to filter out metrics from certain databases according to their database_name tag.
  ## If leave blank, no metric from any database is filtered out.
  # db_filter = ["some_db_instance_name", "other_db_instance_name"]


  ## Run a custom SQL query and collect corresponding metrics.
  #
  # [[inputs.sqlserver.custom_queries]]
  #   sql = '''
  #     select counter_name,cntr_type,cntr_value
  #     from sys.dm_os_performance_counters
  #   '''
  #   metric = "sqlserver_custom_stat"
  #   tags = ["counter_name","cntr_type"]
  #   fields = ["cntr_value"]

  # [inputs.sqlserver.log]
  # files = []
  # #grok pipeline script path
  # pipeline = "sqlserver.p"

  [inputs.sqlserver.tags]
  # some_tag = "some_value"
  # more_tag = "some_other_value"
`

	pipeline = `
grok(_,"%{TIMESTAMP_ISO8601:time} %{NOTSPACE:origin}\\s+%{GREEDYDATA:msg}")
default_time(time, "+0")
`

	inputName   = `sqlserver`
	catalogName = "db"
	l           = logger.DefaultSLogger(inputName)

	collectCache        []*point.Point
	loggingCollectCache []*point.Point

	minInterval = time.Second * 5
	maxInterval = time.Second * 30
	query       = []string{
		sqlServerPerformanceCounters,
		sqlServerWaitStatsCategorized,
		sqlServerDatabaseIO,
		sqlServerProperties,
		sqlServerSchedulers,
		sqlServerVolumeSpace,
		sqlServerDatabaseSize,
		sqlServerDatabaseBackup,
	}
	loggingQuery = []string{
		sqlServerLockTable,
		sqlServerLockRow,
		sqlServerLockDead,
		sqlServerLogicIO,
		sqlServerWorkerTime,
	}
)

type customQuery struct {
	SQL    string   `toml:"sql"`
	Metric string   `toml:"metric"`
	Tags   []string `toml:"tags"`
	Fields []string `toml:"fields"`
}

type Input struct {
	Host        string            `toml:"host"`
	User        string            `toml:"user"`
	Password    string            `toml:"password"`
	Interval    datakit.Duration  `toml:"interval"`
	Tags        map[string]string `toml:"tags"`
	Log         *sqlserverlog     `toml:"log"`
	Database    string            `toml:"database,omitempty"`
	CustomQuery []*customQuery    `toml:"custom_queries"`
	AllowTLS10  bool              `toml:"allow_tls10,omitempty"`

	QueryVersionDeprecated int      `toml:"query_version,omitempty"`
	ExcludeQuery           []string `toml:"exclude_query,omitempty"`

	DBFilter    []string `toml:"db_filter,omitempty"`
	dbFilterMap map[string]struct{}

	lastErr error
	tail    *tailer.Tailer
	start   time.Time
	db      *sql.DB

	Election bool `toml:"election"`
	pauseCh  chan bool
	pause    bool

	semStop *cliutils.Sem // start stop signal
	feeder  dkio.Feeder
	opt     point.Option

	collectFuncs map[string]func() error
}

type sqlserverlog struct {
	Files             []string `toml:"files"`
	Pipeline          string   `toml:"pipeline"`
	IgnoreStatus      []string `toml:"ignore"`
	CharacterEncoding string   `toml:"character_encoding"`
}

func newCountFieldInfo(desc string) *inputs.FieldInfo {
	return &inputs.FieldInfo{
		DataType: inputs.Int,
		Type:     inputs.Count,
		Unit:     inputs.NCount,
		Desc:     desc,
	}
}

func newStringFieldInfo(desc string) *inputs.FieldInfo {
	return &inputs.FieldInfo{
		DataType: inputs.String,
		Type:     inputs.String,
		Unit:     inputs.TODO,
		Desc:     desc,
	}
}

func newTimeFieldInfo(desc string) *inputs.FieldInfo {
	return &inputs.FieldInfo{
		DataType: inputs.Int,
		Type:     inputs.Gauge,
		Unit:     inputs.DurationMS,
		Desc:     desc,
	}
}

func newByteFieldInfo(desc string) *inputs.FieldInfo {
	return &inputs.FieldInfo{
		DataType: inputs.Int,
		Type:     inputs.Gauge,
		Unit:     inputs.SizeByte,
		Desc:     desc,
	}
}

func newKByteFieldInfo(desc string) *inputs.FieldInfo {
	return &inputs.FieldInfo{
		DataType: inputs.Float,
		Type:     inputs.Gauge,
		Unit:     inputs.SizeKB,
		Desc:     desc,
	}
}

func newBoolFieldInfo(desc string) *inputs.FieldInfo {
	return &inputs.FieldInfo{
		DataType: inputs.Bool,
		Type:     inputs.Gauge,
		Unit:     inputs.UnknownUnit,
		Desc:     desc,
	}
}

func obfuscateSQL(text string) string {
	reg := regexp.MustCompile(`\n|\s+`)
	sql := strings.TrimSpace(reg.ReplaceAllString(text, " "))

	if out, err := obfuscate.NewObfuscator(nil).Obfuscate("sql", sql); err != nil {
		l.Debugf("Failed to obfuscate, err: %s \n", err.Error())
		return text
	} else {
		return out.Query
	}
}

func transformData(measurement string, tags map[string]string, fields map[string]interface{}) {
	if tags == nil {
		return
	}
	switch measurement {
	case "sqlserver_lock_dead":
		if field, ok := fields["blocking_text"]; ok {
			if text, isString := field.(string); isString {
				fields["blocking_text"] = obfuscateSQL(text)
				fields["message"] = fields["blocking_text"]
			}
		}
	case "sqlserver_logical_io":
		if field, ok := fields["message"]; ok {
			if text, isString := field.(string); isString {
				fields["message"] = obfuscateSQL(text)
			}
		}
	case "sqlserver_database_size":
		if field, ok := fields["data_size"]; ok {
			if data, isUint := field.([]uint8); isUint {
				if dataSize, err := strconv.ParseFloat(string(data), 64); err == nil {
					fields["data_size"] = dataSize
				}
			}
		}
		if field, ok := fields["log_size"]; ok {
			if data, isUint := field.([]uint8); isUint {
				if dataSize, err := strconv.ParseFloat(string(data), 64); err == nil {
					fields["log_size"] = dataSize
				}
			}
		}
	default:
	}
}
