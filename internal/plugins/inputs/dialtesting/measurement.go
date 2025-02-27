// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

//go:build !windows
// +build !windows

package dialtesting

import (
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
)

type httpMeasurement struct {
	name   string
	tags   map[string]string
	fields map[string]interface{}
}

func (m *httpMeasurement) LineProto() (*point.Point, error) {
	return point.NewPoint(m.name, m.tags, m.fields, point.LOpt())
}

//nolint:lll
func (m *httpMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "http_dial_testing",
		Tags: map[string]interface{}{
			"name":               &inputs.TagInfo{Desc: "拨测名称"},
			"url":                &inputs.TagInfo{Desc: "拨测地址，如 `http://wwww.baidu.com`"},
			"dest_ip":            &inputs.TagInfo{Desc: "目标 IP, 如 127.0.0.1"},
			"country":            &inputs.TagInfo{Desc: "拨测发起所在国家"},
			"province":           &inputs.TagInfo{Desc: "拨测发起所在省份"},
			"city":               &inputs.TagInfo{Desc: "拨测发起所在城市"},
			"internal":           &inputs.TagInfo{Desc: "国内/海外，`true` 表示国内/`false` 表示国外"},
			"isp":                &inputs.TagInfo{Desc: "运营商，电信/移动/联通"},
			"status":             &inputs.TagInfo{Desc: "拨测状态，OK/FAIL"},
			"status_code_class":  &inputs.TagInfo{Desc: "HTTP 状态码，如 `200`"},
			"status_code_string": &inputs.TagInfo{Desc: "HTTP 状态字符串，如 `200 OK`"},
			"proto":              &inputs.TagInfo{Desc: "HTTP 版本，如 `HTTP/1.0`"},
		},
		Fields: map[string]interface{}{
			"status_code": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "web page response code",
			},
			"message": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "包括请求头（`request_header`）/请求体（`request_body`）/返回头（`response_header`）/返回体（`response_body`）/`fail_reason` 会冗余一份",
			},
			"fail_reason": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "拨测失败原因",
			},
			"response_time": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "HTTP 响应时间",
			},
			"response_body_size": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.SizeByte,
				Desc:     "body 长度",
			},
			"success": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "只有 1/-1 两种状态。1 表示成功/-1 表示失败",
			},
			"proto": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "示例 HTTP/1.0",
			},
		},
	}
}

type tcpMeasurement struct {
	name   string
	tags   map[string]string
	fields map[string]interface{}
}

func (m *tcpMeasurement) LineProto() (*point.Point, error) {
	return point.NewPoint(m.name, m.tags, m.fields, point.LOpt())
}

//nolint:lll
func (m *tcpMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "tcp_dial_testing",
		Tags: map[string]interface{}{
			"name":      &inputs.TagInfo{Desc: "拨测名称，如「百度测试」"},
			"dest_host": &inputs.TagInfo{Desc: "示例 `wwww.baidu.com`"},
			"dest_port": &inputs.TagInfo{Desc: "端口号，如 `80`"},
			"dest_ip":   &inputs.TagInfo{Desc: "目标 IP, 如 127.0.0.1"},
			"country":   &inputs.TagInfo{Desc: "国家"},
			"province":  &inputs.TagInfo{Desc: "省份"},
			"city":      &inputs.TagInfo{Desc: "城市"},
			"internal":  &inputs.TagInfo{Desc: "国内/海外，`true` 表示国内/`false` 表示国外"},
			"isp":       &inputs.TagInfo{Desc: "运营商，电信/移动/联通"},
			"status":    &inputs.TagInfo{Desc: "拨测状态，OK/FAIL"},
			"proto":     &inputs.TagInfo{Desc: "协议类型，此处统一为 `tcp`"},
		},
		Fields: map[string]interface{}{
			"message": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "包括响应时间（`response_time`）/错误原因（`fail_reason`）",
			},
			"traceroute": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "路由跟踪数据文本（JSON 格式）",
			},
			"fail_reason": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "拨测失败原因",
			},
			"response_time": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "TCP 连接时间 ",
			},
			"response_time_with_dns": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "连接时间（含 DNS 解析）",
			},
			"success": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "只有 1/-1 两种状态。1 表示成功/-1 表示失败",
			},
		},
	}
}

type icmpMeasurement struct {
	name   string
	tags   map[string]string
	fields map[string]interface{}
}

func (m *icmpMeasurement) LineProto() (*point.Point, error) {
	return point.NewPoint(m.name, m.tags, m.fields, point.LOpt())
}

//nolint:lll
func (m *icmpMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "icmp_dial_testing",
		Tags: map[string]interface{}{
			"name":      &inputs.TagInfo{Desc: "拨测名称，如百度测试"},
			"dest_host": &inputs.TagInfo{Desc: "拨测地址，如 `wwww.baidu.com`"},
			"country":   &inputs.TagInfo{Desc: "拨测发起所在国家，如德国"},
			"province":  &inputs.TagInfo{Desc: "拨测发起所在省份，如浙江"},
			"city":      &inputs.TagInfo{Desc: "拨测发起所在城市，如杭州"},
			"internal":  &inputs.TagInfo{Desc: "国内/海外，`true` 表示国内/`false` 表示国外"},
			"isp":       &inputs.TagInfo{Desc: "运营商，电信/移动/联通"},
			"status":    &inputs.TagInfo{Desc: "拨测状态，OK/FAIL"},
			"proto":     &inputs.TagInfo{Desc: "协议类型，此处统一为 `icmp`"},
		},
		Fields: map[string]interface{}{
			"message": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "包括平均 RTT 时间(average_round_trip_time)/错误原因(fail_reason)",
			},
			"fail_reason": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "拨测失败原因",
			},
			"traceroute": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "路由跟踪数据文本(JSON 格式)",
			},
			"average_round_trip_time": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "平均往返时间(RTT)",
			},
			"average_round_trip_time_in_millis": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationMS,
				Desc:     "平均往返时间(RTT). 本字段将被弃用",
			},
			"min_round_trip_time": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "最小往返时间(RTT)",
			},
			"min_round_trip_time_in_millis": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationMS,
				Desc:     "最小往返时间(RTT). 本字段将被弃用",
			},
			"std_round_trip_time": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "往返时间(RTT)标准差",
			},
			"std_round_trip_time_in_millis": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationMS,
				Desc:     "往返时间（RTT）标准差。本字段将被弃用",
			},
			"max_round_trip_time": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "最大往返时间(RTT)",
			},
			"max_round_trip_time_in_millis": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationMS,
				Desc:     "最大往返时间(RTT). 本字段将被弃用",
			},
			"packet_loss_percent": &inputs.FieldInfo{
				DataType: inputs.Float,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "丢包率",
			},
			"packets_received": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.Count,
				Desc:     "接受的数据包",
			},
			"packets_sent": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.Count,
				Desc:     "发送的数据包",
			},
			"success": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "只有 1/-1 两种状态。1 表示成功/-1 表示失败",
			},
		},
	}
}

type websocketMeasurement struct {
	name   string
	tags   map[string]string
	fields map[string]interface{}
}

func (m *websocketMeasurement) LineProto() (*point.Point, error) {
	return point.NewPoint(m.name, m.tags, m.fields, point.LOpt())
}

//nolint:lll
func (m *websocketMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "websocket_dial_testing",
		Tags: map[string]interface{}{
			"name":     &inputs.TagInfo{Desc: "拨测名称"},
			"url":      &inputs.TagInfo{Desc: "拨测地址，如 `ws://www.abc.com`"},
			"country":  &inputs.TagInfo{Desc: "拨测发起所在国家"},
			"province": &inputs.TagInfo{Desc: "拨测发起所在省份"},
			"city":     &inputs.TagInfo{Desc: "拨测发起所在城市"},
			"internal": &inputs.TagInfo{Desc: "国内/海外，`true` 表示国内/`false` 表示国外"},
			"isp":      &inputs.TagInfo{Desc: "运营商，电信/移动/联通"},
			"status":   &inputs.TagInfo{Desc: "拨测状态，OK/FAIL"},
			"proto":    &inputs.TagInfo{Desc: "协议类型，此处统一为 `websocket`"},
		},
		Fields: map[string]interface{}{
			"message": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "包括响应时间(response_time)/错误原因(fail_reason)",
			},
			"fail_reason": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "拨测失败原因",
			},
			"response_message": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "拨测返回的消息",
			},
			"sent_message": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "拨测发送的消息",
			},
			"response_time": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "连接时间",
			},
			"response_time_with_dns": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.DurationUS,
				Desc:     "连接时间（含 DNS 解析）",
			},
			"success": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "只有 1/-1 两种状态。1 表示成功/-1 表示失败",
			},
		},
	}
}
