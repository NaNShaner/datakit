// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package dk collect Datakit metrics.
package dk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/GuanceCloud/cliutils/logger"
	"github.com/GuanceCloud/cliutils/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/config"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io"
	dkpt "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/prom"
)

const (
	inputName   = "dk"
	source      = "dk-metrics"
	measurement = "dk"
)

var (
	l            = logger.DefaultSLogger(source)
	defaultHost  = "localhost:9529"
	configSample = `
[[inputs.dk]]

  # See https://docs.guance.com/datakit/datakit-metrics/#metrics for all metrics exported by Datakit.
  metric_name_filter = [
    ### Collect all metrics(these may collect 300+ metrics of Datakit)
    ### if you want to collect all, make this rule the first in the list.
    # ".*",

    "datakit_http.*",       # HTTP API
    "datakit_goroutine.*",  # Goroutine

    ### runtime related
    "datakit_cpu_.*",
    "datakit_.*_alloc_bytes", # Memory
    "datakit_open_files",
    "datakit_uptime_seconds",
    "datakit_data_overuse",
    "datakit_process_.*",

    ### election
    "datakit_election_status",

    ### Dataway related
    #"datakit_io_dataway_.*",
    #"datakit_io_http_retry_total",

    ### Filter
    #"datakit_filter_.*",

    ### Input feed
    #".*_feed_.*",
  ]

  # keep empty to collect all types(count/gauge/summary/...)
  metric_types = []

  # collect frequency
  interval = "30s"

[inputs.dk.tags]
   # tag1 = "val-1"
   # tag2 = "val-2"
`
	maxInterval = time.Minute
	minInterval = 5 * time.Second
)

type Input struct {
	MetricFilter []string          `toml:"metric_name_filter"`
	MetricTypes  []string          `toml:"metric_types"`
	Interval     time.Duration     `toml:"interval"`
	Tags         map[string]string `toml:"tags"`

	Tagger dkpt.GlobalTagger `toml:"-"`
	Feeder io.Feeder         `toml:"-"`

	url  string
	prom *prom.Prom
}

// Singleton make the input only 1 instance when multiple instance configured.
func (*Input) Singleton() {}

// ReadEnv accept specific ENV settings to input.
//
//  ENV_INPUT_DK_ENABLE_ALL_METRICS(bool)
//  ENV_INPUT_DK_ADD_METRICS(json-string-list)
//  ENV_INPUT_DK_ONLY_METRICS(json-string-list)
func (i *Input) ReadEnv(envs map[string]string) {
	if _, ok := envs["ENV_INPUT_DK_ENABLE_ALL_METRICS"]; ok {
		i.MetricFilter = nil
	}

	if x := envs["ENV_INPUT_DK_ADD_METRICS"]; x != "" {
		arr := []string{}
		if err := json.Unmarshal([]byte(x), &arr); err != nil {
			l.Warnf("json.Unmarshal: %s, ignored", err)
		} else {
			i.MetricFilter = append(i.MetricFilter, arr...)
		}
	}

	if x := envs["ENV_INPUT_DK_ONLY_METRICS"]; x != "" {
		arr := []string{}
		if err := json.Unmarshal([]byte(x), &arr); err != nil {
			l.Warnf("json.Unmarshal: %s, ignored", err)
		} else {
			i.MetricFilter = arr
		}
	}
}

func (*Input) Terminate() {
	// do nothing
}

func (*Input) Catalog() string {
	return "host"
}

func (i *Input) SampleConfig() string {
	return configSample
}

func (i *Input) setup(listen string) {
	// setup tags
	for k, v := range i.Tagger.HostTags() {
		if _, ok := i.Tags[k]; !ok { // add global tags if not exist.
			l.Infof("add global tag %q:%q", k, v)
			i.Tags[k] = v
		}
	}

	// 'url' not config.Cfg.HTTPAPI.Listen, we force redirect to current listen address
	if u, err := url.Parse(i.url); err == nil {
		if u.Host != listen {
			l.Infof("force redirect URL from %q to %q", u.Host, listen)
			u.Host = listen
			i.url = u.String()
		}
	}
}

func (i *Input) Run() {
	l = logger.SLogger(source)

	i.Interval = config.ProtectedInterval(minInterval, maxInterval, i.Interval)

	i.setup(config.Cfg.HTTPAPI.Listen)

	// init prom
	for {
		x, err := prom.NewProm(
			prom.WithLogger(l),
			prom.WithSource(source),
			prom.WithMetricTypes(i.MetricTypes),
			prom.WithMetricNameFilter(i.MetricFilter),
			prom.WithMeasurementName(measurement),
			prom.WithTags(i.Tags),
		)

		if err != nil {
			l.Errorf("prom.NewProm: %s", err)
			select {
			case <-datakit.Exit.Wait():
				return
			default:
				time.Sleep(time.Second)
			}
		} else {
			i.prom = x
			break
		}
	}

	tick := time.NewTicker(i.Interval)

	for {
		start := time.Now()
		pts, err := i.prom.CollectFromHTTPV2(i.url)
		if err != nil {
			l.Warnf("prom.CollectFromHTTPV2: %s, ignored", err.Error())
			i.Feeder.FeedLastError(source, err.Error(), point.Metric)
		} else {
			if err := i.Feeder.Feed(source, point.Metric,
				pts,
				&io.Option{
					CollectCost: time.Since(start),
				}); err != nil {
				l.Warn("Feed: %s, ignored", err)
			}
		}

		select {
		case <-tick.C:
		case <-datakit.Exit.Wait():
			return
		}
	}
}

func (*Input) AvailableArchs() []string {
	return datakit.AllOS
}

func def() *Input {
	return &Input{
		Feeder:   io.DefaultFeeder(),
		url:      fmt.Sprintf("http://%s/metrics", defaultHost),
		Interval: time.Second * 30,
		Tags:     map[string]string{},
		Tagger:   dkpt.DefaultGlobalTagger(),
	}
}

func init() { //nolint:gochecknoinits
	inputs.Add(inputName, func() inputs.Input {
		return def()
	})
}
