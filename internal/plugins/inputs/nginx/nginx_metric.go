// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package nginx

import (
	"fmt"
	"time"

	"github.com/GuanceCloud/cliutils/point"
	dkpt "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
)

type NginxMeasurement struct {
	name   string
	tags   map[string]string
	fields map[string]interface{}
	ts     time.Time
	ipt    *Input
}

// Point implement MeasurementV2.
func (m *NginxMeasurement) Point() *point.Point {
	opts := point.DefaultMetricOptions()
	opts = append(opts, point.WithTime(m.ts), m.ipt.opt)

	return point.NewPointV2([]byte(m.name),
		append(point.NewTags(m.tags), point.NewKVs(m.fields)...),
		opts...)
}

func (m *NginxMeasurement) LineProto() (*dkpt.Point, error) {
	// return point.NewPoint(m.name, m.tags, m.fields, dkpt.MOptElectionV2(m.election))
	return nil, fmt.Errorf("not implement")
}

//nolint:lll
func (m *NginxMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: nginx,
		Fields: map[string]interface{}{
			"load_timestamp":      newOtherFieldInfo(inputs.Int, inputs.Gauge, inputs.TimestampMS, "nginx process load time in milliseconds, exist when using vts"),
			"connection_active":   newCountFieldInfo("The current number of active client connections"),
			"connection_reading":  newCountFieldInfo("The total number of reading client connections"),
			"connection_writing":  newCountFieldInfo("The total number of writing client connections"),
			"connection_waiting":  newCountFieldInfo("The total number of waiting client connections"),
			"connection_handled":  newCountFieldInfo("The total number of handled client connections"),
			"connection_requests": newCountFieldInfo("The total number of requests client connections"),
			"connection_accepts":  newCountFieldInfo("The total number of accepts client connections"),
		},
		Tags: map[string]interface{}{
			"nginx_server":  inputs.NewTagInfo("nginx server host"),
			"nginx_port":    inputs.NewTagInfo("nginx server port"),
			"host":          inputs.NewTagInfo("host name which installed nginx"),
			"nginx_version": inputs.NewTagInfo("nginx version, exist when using vts"),
		},
	}
}
