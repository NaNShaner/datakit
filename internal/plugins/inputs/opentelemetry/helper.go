// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package opentelemetry

import (
	"regexp"

	common "github.com/GuanceCloud/tracing-protos/opentelemetry-gen-go/common/v1"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io/point"
)

type getAttributeFunc func(key string, attributes []*common.KeyValue) (*common.KeyValue, bool)

func getAttr(key string, attributes []*common.KeyValue) (*common.KeyValue, bool) {
	for _, attr := range attributes {
		if attr.Key == key {
			return attr, true
		}
	}

	return nil, false
}

func getAttrWrapper(ignore []*regexp.Regexp) getAttributeFunc {
	if len(ignore) == 0 {
		return getAttr
	} else {
		return func(key string, attributes []*common.KeyValue) (*common.KeyValue, bool) {
			for _, rexp := range ignore {
				if rexp.MatchString(key) {
					return nil, false
				}
			}

			return getAttr(key, attributes)
		}
	}
}

type extractAttributesFunc func(src []*common.KeyValue) (dest []*common.KeyValue)

func extractAttrs(src []*common.KeyValue) (dest []*common.KeyValue) {
	dest = append(dest, src...)

	return
}

func extractAttrsWrapper(ignore []*regexp.Regexp) extractAttributesFunc {
	if len(ignore) == 0 {
		return extractAttrs
	} else {
		return func(src []*common.KeyValue) (dest []*common.KeyValue) {
		NEXT_ATTR:
			for _, v := range src {
				for _, rexp := range ignore {
					if rexp.MatchString(v.Key) {
						continue NEXT_ATTR
					}
				}
				dest = append(dest, v)
			}

			return
		}
	}
}

func newAttributes(attrs []*common.KeyValue) *attributes {
	a := &attributes{}
	a.attrs = append(a.attrs, attrs...)

	return a
}

type attributes struct {
	attrs []*common.KeyValue
}

// nolint: deadcode,unused
func (a *attributes) loop(proc func(i int, k string, v *common.KeyValue) bool) {
	for i, v := range a.attrs {
		if !proc(i, v.Key, v) {
			break
		}
	}
}

func (a *attributes) merge(attrs ...*common.KeyValue) *attributes {
	for _, v := range attrs {
		if _, i := a.find(v.Key); i != -1 {
			a.attrs[i] = v
		} else {
			a.attrs = append(a.attrs, v)
		}
	}

	return a
}

func (a *attributes) find(key string) (*common.KeyValue, int) {
	for i := len(a.attrs) - 1; i >= 0; i-- {
		if a.attrs[i].Key == key {
			return a.attrs[i], i
		}
	}

	return nil, -1
}

func (a *attributes) remove(key string) *attributes {
	if _, i := a.find(key); i != -1 {
		a.attrs = append(a.attrs[:i], a.attrs[i+1:]...)
	}

	return a
}

func (a *attributes) splite() (map[string]string, map[string]interface{}) {
	tags := make(map[string]string)
	metrics := make(map[string]interface{})
	for _, v := range a.attrs {
		switch v.Value.Value.(type) {
		case *common.AnyValue_BytesValue, *common.AnyValue_StringValue:
			if s := v.Value.GetStringValue(); len(s) > point.MaxTagValueLen {
				metrics[v.Key] = s
			} else {
				tags[v.Key] = s
			}
		case *common.AnyValue_DoubleValue:
			metrics[v.Key] = v.Value.GetDoubleValue()
		case *common.AnyValue_IntValue:
			metrics[v.Key] = v.Value.GetIntValue()
		}
	}

	return tags, metrics
}
