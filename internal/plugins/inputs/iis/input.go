// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

//go:build windows && amd64
// +build windows,amd64

package iis

import (
	"context"
	"fmt"
	"time"

	"github.com/GuanceCloud/cliutils"
	"github.com/GuanceCloud/cliutils/logger"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/config"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/goroutine"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/tailer"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/win_utils/pdh"
	"golang.org/x/sys/windows"
)

const (
	minInterval = time.Second * 5
	maxInterval = time.Minute * 10
)

var (
	inputName                           = "iis"
	metricNameWebService                = "iis_web_service"
	metricNameAppPoolWas                = "iis_app_pool_was"
	l                                   = logger.DefaultSLogger("iis")
	_                    inputs.InputV2 = (*Input)(nil)
)

type Input struct {
	Interval datakit.Duration

	Tags map[string]string

	Log  *iisLog `toml:"log"`
	tail *tailer.Tailer

	collectCache []inputs.Measurement

	semStop *cliutils.Sem // start stop signal
}

type iisLog struct {
	Files    []string `toml:"files"`
	Pipeline string   `toml:"pipeline"`
}

func (i *Input) Catalog() string {
	return "iis"
}

func (i *Input) SampleConfig() string {
	return sampleConfig
}

func (i *Input) SampleMeasurement() []inputs.Measurement {
	return []inputs.Measurement{
		&IISAppPoolWas{},
		&IISWebService{},
	}
}

// RunPipeline TODO.
func (i *Input) RunPipeline() {
	if i.Log == nil || len(i.Log.Files) == 0 {
		return
	}

	opt := &tailer.Option{
		Source:     "iis",
		Service:    "iis",
		Pipeline:   i.Log.Pipeline,
		GlobalTags: i.Tags,
		Done:       i.semStop.Wait(),
	}

	var err error
	if i.tail, err = tailer.NewTailer(i.Log.Files, opt); err != nil {
		l.Error(err)
		io.FeedLastError(inputName, err.Error())
		return
	}

	g := goroutine.NewGroup(goroutine.Option{Name: "inputs_iis"})
	g.Go(func(ctx context.Context) error {
		i.tail.Start()
		return nil
	})
}

func (*Input) PipelineConfig() map[string]string {
	pipelineConfig := map[string]string{
		inputName: pipelineCfg,
	}
	return pipelineConfig
}

func (i *Input) GetPipeline() []*tailer.Option {
	return []*tailer.Option{
		{
			Source:  inputName,
			Service: inputName,
			Pipeline: func() string {
				if i.Log != nil {
					return i.Log.Pipeline
				}
				return ""
			}(),
		},
	}
}

func (i *Input) AvailableArchs() []string {
	return []string{datakit.OSLabelWindows}
}

func (i *Input) Run() {
	l = logger.SLogger(inputName)

	l.Infof("iis input started")

	i.Interval.Duration = config.ProtectedInterval(minInterval, maxInterval, i.Interval.Duration)
	tick := time.NewTicker(i.Interval.Duration)

	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			start := time.Now()
			if err := i.Collect(); err == nil {
				if feedErr := inputs.FeedMeasurement(inputName, datakit.Metric, i.collectCache,
					&io.Option{CollectCost: time.Since(start)}); feedErr != nil {
					l.Error(feedErr)
					io.FeedLastError(inputName, feedErr.Error())
				}
			} else {
				l.Error(err)
				io.FeedLastError(inputName, err.Error())
			}
			i.collectCache = make([]inputs.Measurement, 0)
		case <-datakit.Exit.Wait():
			i.exit()
			l.Infof("iis input exit")
			return

		case <-i.semStop.Wait():
			i.exit()
			l.Infof("iis input return")
			return
		}
	}
}

func (i *Input) exit() {
	if i.tail != nil {
		i.tail.Close()
		l.Infof("iis logging exit")
	}
}

func (i *Input) Terminate() {
	if i.semStop != nil {
		i.semStop.Close()
	}
}

func (i *Input) Collect() error {
	for mName, metricCounterMap := range PerfObjMetricMap {
		for objName := range metricCounterMap {
			// measurement name -> instance name -> metric name -> counter query handle list index
			indexMap := map[string]map[string]map[string]int{mName: {}}

			// counter name is localized and cannot be used
			instanceList, _, ret := pdh.PdhEnumObjectItems(objName)
			if ret != uint32(windows.ERROR_SUCCESS) {
				return fmt.Errorf("failed to enumerate the instance and counter of object %s", objName)
			}

			pathList := make([]string, 0)
			pathListIndex := 0
			// instance
			for i := 0; i < len(instanceList); i++ {
				indexMap[mName][instanceList[i]] = map[string]int{}
				for keyCounter := range metricCounterMap[objName] {
					if metricName, ok := metricCounterMap[objName][keyCounter]; ok {
						// make full counter path
						tmpCounterFullPath := pdh.MakeFullCounterPath(objName, instanceList[i], keyCounter)
						pathList = append(pathList, tmpCounterFullPath)

						indexMap[mName][instanceList[i]][metricName] = pathListIndex
						pathListIndex += 1
					}
				}
			}
			if len(pathList) < 1 {
				return fmt.Errorf("obj %s no vaild counter ", objName)
			}
			var handle pdh.PDH_HQUERY
			var counterHandle pdh.PDH_HCOUNTER
			if ret = pdh.PdhOpenQuery(0, 0, &handle); ret != uint32(windows.ERROR_SUCCESS) {
				return fmt.Errorf("object: %s, PdhOpenQuery return: %x", objName, ret)
			}

			counterHandleList := make([]pdh.PDH_HCOUNTER, len(pathList))
			valueList := make([]interface{}, len(pathList))
			for i := range pathList {
				ret = pdh.PdhAddEnglishCounter(handle, pathList[i], 0, &counterHandle)
				counterHandleList[i] = counterHandle
				if ret != uint32(windows.ERROR_SUCCESS) {
					return fmt.Errorf("add query counter %s failed", pathList[i])
				}
			}
			// Call PDH query function,
			// for some counter, it need to call twice
			if ret = pdh.PdhCollectQueryData(handle); ret != uint32(windows.ERROR_SUCCESS) {
				return fmt.Errorf("object: %s, PdhCollectQueryData return: %x", objName, ret)
			}

			// If object name is `Web Service` and only call the func once,
			// will cause func such as PdhGetFormattedCounterValueDouble
			// return PDH_INVALID_DATA
			if ret = pdh.PdhCollectQueryData(handle); ret != uint32(windows.ERROR_SUCCESS) {
				return fmt.Errorf("object: %s, PdhCollectQueryData return: %x", objName, ret)
			}

			// Get value
			var counterValue pdh.PDH_FMT_COUNTERVALUE_DOUBLE
			for i := 0; i < len(counterHandleList); i++ {
				ret = pdh.PdhGetFormattedCounterValueDouble(counterHandleList[i], nil, &counterValue)
				if ret != uint32(windows.ERROR_SUCCESS) {
					return fmt.Errorf("PdhGetFormattedCounterValueDouble return: %x\n\t"+
						"CounterFullPath: %s", ret, pathList[i])
				}
				valueList[i] = counterValue.DoubleValue
			}

			// Close query
			ret = pdh.PdhCloseQuery(handle)
			if ret != uint32(windows.ERROR_SUCCESS) {
				return fmt.Errorf("object: %s, PdhCloseQuery return: %x", objName, ret)
			}

			for instanceName := range indexMap[mName] {
				tags := map[string]string{}
				fields := map[string]interface{}{}
				for k, v := range i.Tags {
					tags[k] = v
				}
				for metricName := range indexMap[mName][instanceName] {
					fields[metricName] = valueList[indexMap[mName][instanceName][metricName]]
				}
				switch objName {
				case "Web Service":
					tags["website"] = instanceName
				case "APP_POOL_WAS":
					tags["app_pool"] = instanceName
				default:
					return fmt.Errorf("action not defined, obj name: %s  measurement name: %s", objName, mName)
				}
				i.collectCache = append(i.collectCache, &measurement{
					name:   mName,
					tags:   tags,
					fields: fields,
				})
			}
		}
	}
	return nil
}

func init() { // nolint:gochecknoinits
	inputs.Add(inputName, func() inputs.Input {
		return &Input{
			Interval: datakit.Duration{Duration: time.Second * 15},

			semStop: cliutils.NewSem(),
		}
	})
}
