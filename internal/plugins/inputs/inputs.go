// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package inputs manage all input's interfaces.
package inputs

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/GuanceCloud/cliutils/logger"
	"github.com/GuanceCloud/cliutils/system/rtpanic"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/tailer"
)

const (
	ElectionPauseTimeout       = time.Second * 15
	ElectionResumeTimeout      = time.Second * 15
	ElectionPauseChannelLength = 8
)

var (
	Inputs         = map[string]Creator{}
	InputsInfo     = map[string][]*inputInfo{}
	ConfigInfo     = map[string]*Config{}
	ConfigFileHash = map[string]struct{}{}
	panicInputs    = map[string]int{}
	mtx            = sync.RWMutex{}
	l              = logger.DefaultSLogger("inputs")
)

func GetElectionInputs() map[string][]ElectionInput {
	res := make(map[string][]ElectionInput)
	for k, arr := range InputsInfo {
		for _, x := range arr {
			if y, ok := x.input.(ElectionInput); ok {
				if z, ok := x.input.(ElectionEnabler); ok {
					if !z.ElectionEnabled() {
						l.Debugf("skip election disabled input: %s", k)
						continue
					}
				}
				l.Debugf("find election inputs %s", k)
				res[k] = append(res[k], y)
			}
		}
	}

	return res
}

type ConfigPathStat struct {
	Loaded int8   `json:"loaded"` // 0: 启动失败 1: 启动成功 2: 修改未加载
	Path   string `json:"path"`
}

type Config struct {
	ConfigPaths  []*ConfigPathStat `json:"config_paths"`
	SampleConfig string            `json:"sample_config"`
	Catalog      string            `json:"catalog"`
	ConfigDir    string            `json:"config_dir"`
}

type Input interface {
	Catalog() string
	Run()
	SampleConfig() string
}

type HTTPInput interface {
	RegHTTPHandler()
}

// Dashboard used to export inputs dashboard JSON.
type Dashboard interface {
	Dashboard(lang I18n) map[string]string
	// List return all related dashboard name
	// if nothing returned, use input name
	DashboardList() []string
}

// Monitor used to export inputs monitor JSON.
type Monitor interface {
	Monitor(lang I18n) map[string]string
	// List return all related monitor name
	// if nothing returned, use input name
	MonitorList() []string
}

type Singleton interface {
	Singleton()
}

type PipelineInput interface {
	PipelineConfig() map[string]string
	RunPipeline()
	GetPipeline() []*tailer.Option
}

type OptionalInput interface {
	SetTags(map[string]string)
}

type XLog struct {
	Files    []string `toml:"files"`
	Pipeline string   `toml:"pipeline"`
	Source   string   `toml:"source"`
	Service  string   `toml:"service"`
}

// InputV2 new input interface got extra interfaces, for better documentation.
type InputV2 interface {
	Input
	SampleMeasurement() []Measurement
	AvailableArchs() []string
	Terminate()
}

type ElectionInput interface {
	Pause() error
	Resume() error
}

type ElectionEnabler interface {
	ElectionEnabled() bool
}

type ReadEnv interface {
	ReadEnv(map[string]string)
}

type LogExampler interface {
	LogExamples() map[string]map[string]string
}

type Creator func() Input

func Add(name string, creator Creator) {
	if _, ok := Inputs[name]; ok {
		l.Fatalf("inputs %s exist(from datakit)", name)
	}

	Inputs[name] = creator

	AddConfigInfoPath(name, "", 0)
}

type inputInfo struct {
	input Input
}

func (ii *inputInfo) Run() {
	if ii.input == nil {
		return
	}

	switch ii.input.(type) {
	case Input:
		ii.input.Run()
	default:
		l.Errorf("invalid input type")
	}
}

func GetInput() ([]byte, error) {
	inputs := make(map[string][]Input)

	mtx.Lock()
	defer mtx.Unlock()

	for name, info := range InputsInfo {
		for _, ii := range info {
			inputs[name] = append(inputs[name], ii.input)
		}
	}

	b, err := json.Marshal(inputs)
	if err != nil {
		l.Errorf("marshal error:%s", err)
		return nil, err
	}
	return b, nil
}

// AddConfigInfoPath add or update input info.
//  if fp is empty, add new config when inputName not exist, or set ConfigPaths empty when exist.
func AddConfigInfoPath(inputName string, fp string, loaded int8) {
	if c, ok := ConfigInfo[inputName]; ok {
		if len(fp) == 0 {
			c.ConfigPaths = []*ConfigPathStat{} // set empty for reload datakit
			return
		}
		for _, p := range c.ConfigPaths {
			if p.Path == fp {
				p.Loaded = loaded
				return
			}
		}
		c.ConfigPaths = append(c.ConfigPaths, &ConfigPathStat{Loaded: loaded, Path: fp})
	} else {
		creator, ok := Inputs[inputName]
		if ok {
			config := &Config{
				ConfigPaths:  []*ConfigPathStat{},
				SampleConfig: creator().SampleConfig(),
				Catalog:      creator().Catalog(),
				ConfigDir:    datakit.ConfdDir,
			}
			if len(fp) > 0 {
				config.ConfigPaths = append(config.ConfigPaths, &ConfigPathStat{Loaded: loaded, Path: fp})
			}
			ConfigInfo[inputName] = config
		}
	}
}

// DeleteConfigInfoPath remove fp from config paths of selected input.
func DeleteConfigInfoPath(inputName, fp string) {
	mtx.Lock()
	defer mtx.Unlock()
	if i, ok := ConfigInfo[inputName]; ok {
		for j, f := range i.ConfigPaths {
			if f.Path == fp {
				i.ConfigPaths = append(i.ConfigPaths[:j], i.ConfigPaths[j+1:]...)
			}
		}
	}
}

func AddInput(name string, input Input) {
	mtx.Lock()
	defer mtx.Unlock()

	// 单例采集器只添加一次
	if _, ok := input.(Singleton); ok {
		if len(InputsInfo[name]) > 0 {
			return
		}
	}
	InputsInfo[name] = append(InputsInfo[name], &inputInfo{input: input})

	l.Debugf("add input %s, total %d", name, len(InputsInfo[name]))
}

func RemoveInput(name string, input Input) {
	mtx.Lock()
	defer mtx.Unlock()

	oldList, ok := InputsInfo[name]
	if !ok {
		return
	}

	newList := []*inputInfo{}

	for _, ii := range oldList {
		if !reflect.DeepEqual(input, ii.input) {
			newList = append(newList, ii)
		}
	}

	InputsInfo[name] = newList

	l.Debugf("remove input %s, current total %d", name, len(InputsInfo[name]))
}

func ResetInputs() {
	mtx.Lock()
	defer mtx.Unlock()
	InputsInfo = map[string][]*inputInfo{}

	// only reset input config path
	for _, v := range ConfigInfo {
		v.ConfigPaths = v.ConfigPaths[0:0]
		v.ConfigDir = datakit.ConfdDir
	}

	ConfigFileHash = map[string]struct{}{}
}

func getEnvs() map[string]string {
	envs := map[string]string{}
	for _, v := range os.Environ() {
		arr := strings.SplitN(v, "=", 2)
		if len(arr) != 2 {
			continue
		}

		if strings.HasPrefix(arr[0], "ENV_") || strings.HasPrefix(arr[0], "DK_") {
			envs[arr[0]] = arr[1]
		}
	}

	return envs
}

func RunInputs() error {
	mtx.RLock()
	defer mtx.RUnlock()
	g := datakit.G("inputs")

	envs := getEnvs()

	for name, arr := range InputsInfo {
		if len(arr) > 1 {
			if _, ok := arr[0].input.(Singleton); ok {
				arr = arr[:1]
			}
		}

		inputInstanceVec.WithLabelValues(name).Set(float64(len(arr)))
		for _, ii := range arr {
			if ii.input == nil {
				l.Debugf("skip non-datakit-input %s", name)
				continue
			}

			if inp, ok := ii.input.(ReadEnv); ok && datakit.Docker {
				inp.ReadEnv(envs)
			}

			if inp, ok := ii.input.(HTTPInput); ok {
				inp.RegHTTPHandler()
			}

			if inp, ok := ii.input.(PipelineInput); ok {
				inp.RunPipeline()
			}

			func(name string, ii *inputInfo) {
				g.Go(func(ctx context.Context) error {
					// NOTE: 让每个采集器间歇运行，防止每个采集器扎堆启动，导致主机资源消耗出现规律性的峰值
					tick := time.NewTicker(time.Duration(rand.Int63n(int64(10 * time.Second)))) //nolint:gosec
					defer tick.Stop()
					select {
					case <-tick.C:
						l.Infof("starting input %s ...", name)

						protectRunningInput(name, ii)

						l.Infof("input %s exited", name)
						return nil
					case <-datakit.Exit.Wait():
						l.Infof("start input %s interrupted", name)
					}
					return nil
				})
			}(name, ii)
		}
	}
	return nil
}

func RunInputExtra() error {
	mtx.RLock()
	defer mtx.RUnlock()

	for name, arr := range InputsInfo {
		for _, ii := range arr {
			if ii.input == nil {
				l.Debugf("skip non-datakit-input %s", name)
				continue
			}

			if inp, ok := ii.input.(HTTPInput); ok {
				inp.RegHTTPHandler()
			}
		}
	}
	return nil
}

func StopInputs() error {
	mtx.RLock()
	defer mtx.RUnlock()

	for name, arr := range InputsInfo {
		for _, ii := range arr {
			if ii.input == nil {
				l.Debugf("skip non-datakit-input %s", name)
				continue
			}

			if inp, ok := ii.input.(InputV2); ok {
				inp.Terminate()
			}
		}
	}
	return nil
}

var MaxCrash = 6

func protectRunningInput(name string, ii *inputInfo) {
	var f rtpanic.RecoverCallback
	crashTime := []string{}

	f = func(trace []byte, err error) {
		defer rtpanic.Recover(f, nil)

		if trace != nil {
			l.Warnf("input %s panic err: %v", name, err)
			l.Warnf("input %s panic trace:\n%s", name, string(trace))

			inputsPanicVec.WithLabelValues(name).Inc()

			crashTime = append(crashTime, fmt.Sprintf("%v", time.Now()))
			addPanic(name)

			io.FeedLastError("crach_"+name, string(trace))

			if len(crashTime) >= MaxCrash {
				l.Warnf("input %s crash %d times(at %+#v), exit now.",
					name, len(crashTime), strings.Join(crashTime, "\n"))

				io.FeedLastError("crash_"+name,
					fmt.Sprintf("input '%s' has exceeded the max crash times %v and it will be stopped.", name, MaxCrash))

				return
			}
		}

		select {
		case <-datakit.Exit.Wait(): // check if datakit exited now
			return
		default:
			ii.Run()
		}
	}

	f(nil, nil)
}

func GetPanicCnt(name string) int {
	mtx.RLock()
	defer mtx.RUnlock()

	return panicInputs[name]
}

func addPanic(name string) {
	mtx.Lock()
	defer mtx.Unlock()

	panicInputs[name]++
}

func InputEnabled(name string) (n int) {
	mtx.RLock()
	defer mtx.RUnlock()
	arr, ok := InputsInfo[name]
	if !ok {
		return
	}

	n = len(arr)
	return
}

// MergeTags merge all optional tags from global tags/inputs config tags and host tags
// from remote URL.
func MergeTags(global, origin map[string]string, remote string) map[string]string {
	out := map[string]string{}

	for k, v := range origin {
		out[k] = v
	}

	host := remote
	if remote == "" {
		goto end
	}

	// if 'host' exist in origin tags, ignore 'host' tag within remote
	if _, ok := origin["host"]; ok {
		goto end
	}

	// try get 'host' tag from remote URL.
	if u, err := url.Parse(remote); err == nil && u.Host != "" { // like scheme://host:[port]/...
		host = u.Host
		if ip, _, err := net.SplitHostPort(u.Host); err == nil {
			host = ip
		}
	} else { // not URL, only IP:Port
		if ip, _, err := net.SplitHostPort(remote); err == nil {
			host = ip
		}
	}

	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		out["host"] = host
	}

end: // global tags(host/election tags) got the lowest priority.
	for k, v := range global {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}

	return out
}

func Init() {
	l = logger.SLogger("inputs")
}
