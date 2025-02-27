// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package profile datakit collector
package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GuanceCloud/cliutils"
	"github.com/GuanceCloud/cliutils/logger"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/config"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/goroutine"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/httpapi"
	dkio "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/trace"
)

const (
	inputName              = "profile"
	profileMaxSize         = 1 << 23
	workspaceUUIDHeaderKey = "X-Datakit-Workspace"
	profileIDHeaderKey     = "X-Datakit-ProfileID"
	timestampHeaderKey     = "X-Datakit-UnixNano"
	sampleConfig           = `
[[inputs.profile]]
  ## profile Agent endpoints register by version respectively.
  ## Endpoints can be skipped listen by remove them from the list.
  ## Default value set as below. DO NOT MODIFY THESE ENDPOINTS if not necessary.
  endpoints = ["/profiling/v1/input"]

  ## set true to enable election, pull mode only
  election = true

## go pprof config
## collect profiling data in pull mode
#[[inputs.profile.go]]
  ## pprof url
  #url = "http://localhost:6060"

  ## pull interval, should be greater or equal than 10s
  #interval = "10s"

  ## service name
  #service = "go-demo"

  ## app env
  #env = "dev"

  ## app version
  #version = "0.0.0"

  ## types to pull
  ## values: cpu, goroutine, heap, mutex, block
  #enabled_types = ["cpu","goroutine","heap","mutex","block"]

#[inputs.profile.go.tags]
  # tag1 = "val1"

## pyroscope config
#[[inputs.profile.pyroscope]]
  ## listen url
  #url = "0.0.0.0:4040"

  ## service name
  #service = "pyroscope-demo"

  ## app env
  #env = "dev"

  ## app version
  #version = "0.0.0"

#[inputs.profile.pyroscope.tags]
  #tag1 = "val1"
`
)

var (
	log = logger.DefaultSLogger(inputName)

	_ inputs.HTTPInput     = &Input{}
	_ inputs.InputV2       = &Input{}
	_ inputs.ElectionInput = (*Input)(nil)

	pointCache = newProfileCache(32, 4096)
)

//nolint:unused
type pyroscopeOpts struct {
	URL     string            `toml:"url"`
	Service string            `toml:"service"`
	Env     string            `toml:"env"`
	Version string            `toml:"version"`
	Tags    map[string]string `toml:"tags"`

	tags      map[string]string
	input     *Input
	cacheData sync.Map // key: name, value: *cacheDetail
}

type profileCache struct {
	pointsMap map[string]*profileBase
	heap      *minHeap
	maxSize   int
	lock      *sync.Mutex // lock: map and heap access lock
}

type minHeap struct {
	buckets []*profileBase
	indexes map[*profileBase]int
}

func newMinHeap(initCap int) *minHeap {
	return &minHeap{
		buckets: make([]*profileBase, 0, initCap),
		indexes: make(map[*profileBase]int, initCap),
	}
}

func (mh *minHeap) Swap(i, j int) {
	mh.indexes[mh.buckets[i]], mh.indexes[mh.buckets[j]] = j, i
	mh.buckets[i], mh.buckets[j] = mh.buckets[j], mh.buckets[i]
}

func (mh *minHeap) Less(i, j int) bool {
	return mh.buckets[i].birth.Before(mh.buckets[j].birth)
}

func (mh *minHeap) Len() int {
	return len(mh.buckets)
}

func (mh *minHeap) siftUp(idx int) {
	if idx >= len(mh.buckets) {
		errMsg := fmt.Sprintf("siftUp: index[%d] out of bounds[%d]", idx, len(mh.buckets))
		log.Error(errMsg)
		panic(errMsg)
	}

	for idx > 0 {
		parent := (idx - 1) / 2

		if !mh.Less(idx, parent) {
			break
		}

		// Swap
		mh.Swap(idx, parent)
		idx = parent
	}
}

func (mh *minHeap) siftDown(idx int) {
	for {
		left := idx*2 + 1
		if left >= mh.Len() {
			break
		}

		minIdx := idx
		if mh.Less(left, minIdx) {
			minIdx = left
		}

		right := left + 1
		if right < mh.Len() && mh.Less(right, minIdx) {
			minIdx = right
		}

		if minIdx == idx {
			break
		}

		mh.Swap(idx, minIdx)
		idx = minIdx
	}
}

func (mh *minHeap) push(pb *profileBase) {
	mh.buckets = append(mh.buckets, pb)
	mh.indexes[pb] = mh.Len() - 1
	mh.siftUp(mh.Len() - 1)
}

func (mh *minHeap) pop() *profileBase {
	if mh.Len() == 0 {
		return nil
	}

	top := mh.getTop()
	mh.remove(top)
	return top
}

func (mh *minHeap) remove(pb *profileBase) {
	idx, ok := mh.indexes[pb]
	if !ok {
		log.Errorf("pb not found in the indexes, profileID = %s", pb.profileID)
		return
	}
	if idx >= mh.Len() {
		errMsg := fmt.Sprintf("remove: index[%d] out of bounds [%d]", idx, mh.Len())
		log.Error(errMsg)
		panic(errMsg)
	}

	if mh.buckets[idx] != pb {
		errMsg := fmt.Sprintf("remove: idx of the buckets[%p] not equal the removing target[%p]", mh.buckets[idx], pb)
		log.Error(errMsg)
		panic(errMsg)
	}
	// delete the idx
	mh.buckets[idx] = mh.buckets[mh.Len()-1]
	mh.indexes[mh.buckets[idx]] = idx
	mh.buckets = mh.buckets[:mh.Len()-1]

	if idx < mh.Len() {
		mh.siftDown(idx)
	}
	delete(mh.indexes, pb)
}

func (mh *minHeap) getTop() *profileBase {
	if mh.Len() == 0 {
		return nil
	}
	return mh.buckets[0]
}

type profileBase struct {
	profileID string
	birth     time.Time
	point     *point.Point
}

func newProfileCache(initCap int, maxCap int) *profileCache {
	if initCap < 32 {
		initCap = 32
	} else if initCap > 256 {
		initCap = 256
	}

	if maxCap < initCap {
		maxCap = initCap
	} else if maxCap > 8196 {
		maxCap = 8196
	}

	return &profileCache{
		pointsMap: make(map[string]*profileBase, initCap),
		heap:      newMinHeap(initCap),
		maxSize:   maxCap,
		lock:      &sync.Mutex{},
	}
}

func (pc *profileCache) push(profileID string, birth time.Time, point *point.Point) {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	if pc.heap.Len() >= pc.maxSize {
		pb := pc.heap.pop()
		if pb != nil {
			delete(pc.pointsMap, pb.profileID)

			log.Warnf("由于达到cache存储数量上限，最早的point数据被丢弃，profileID = [%s], profileTime = [%s]",
				pb.profileID, pb.birth.Format(time.RFC3339))
		}
	}

	newPB := &profileBase{
		profileID: profileID,
		birth:     birth,
		point:     point,
	}

	pc.pointsMap[profileID] = newPB
	pc.heap.push(newPB)
}

func (pc *profileCache) drop(profileID string) *point.Point {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	if pb, ok := pc.pointsMap[profileID]; ok {
		delete(pc.pointsMap, profileID)
		pc.heap.remove(pb)

		if len(pc.pointsMap) != pc.heap.Len() {
			log.Warnf("cache map size do not equals heap size, map size = [%d], heap size = [%d]",
				len(pc.pointsMap), pc.heap.Len())
		}
		return pb.point
	}
	return nil
}

func init() { //nolint:gochecknoinits
	inputs.Add(inputName, func() inputs.Input {
		return &Input{
			pauseCh:  make(chan bool, inputs.ElectionPauseChannelLength),
			Election: true,
			semStop:  cliutils.NewSem(),
		}
	})
}

type Input struct {
	Endpoints      []string         `toml:"endpoints"`
	Go             []*GoProfiler    `toml:"go"`
	PyroscopeLists []*pyroscopeOpts `toml:"pyroscope"`

	Election bool `toml:"election"`
	pause    bool
	pauseCh  chan bool

	semStop *cliutils.Sem // start stop signal
}

func (i *Input) Pause() error {
	tick := time.NewTicker(inputs.ElectionPauseTimeout)
	defer tick.Stop()
	select {
	case i.pauseCh <- true:
		return nil
	case <-tick.C:
		return fmt.Errorf("pause %s failed", inputName)
	}
}

func (i *Input) Resume() error {
	tick := time.NewTicker(inputs.ElectionResumeTimeout)
	defer tick.Stop()
	select {
	case i.pauseCh <- false:
		return nil
	case <-tick.C:
		return fmt.Errorf("resume %s failed", inputName)
	}
}

func (i *Input) ElectionEnabled() bool {
	return i.Election
}

// uploadResponse {"content":{"profileID":"fa9c3d16-1cfc-4e37-950d-129cbebd1cdb"}}.
type uploadResponse struct {
	Content *struct {
		ProfileID string `json:"profileID"`
	} `json:"content"`
}

func profilingProxyURL() (*url.URL, *http.Transport, error) {
	lastErr := fmt.Errorf("no dataway endpoint available now")

	endpoints := config.Cfg.Dataway.GetAvailableEndpoints()

	if len(endpoints) == 0 {
		return nil, nil, lastErr
	}

	for _, ep := range endpoints {
		rawURL, ok := ep.GetCategoryURL()[datakit.ProfilingUpload]
		if !ok || rawURL == "" {
			lastErr = fmt.Errorf("profiling upload url empty")
			continue
		}

		URL, err := url.Parse(rawURL)
		if err != nil {
			lastErr = fmt.Errorf("profiling upload url [%s] parse err:%w", rawURL, err)
			continue
		}
		return URL, ep.Transport(), nil
	}
	return nil, nil, lastErr
}

type reverseProxy struct {
	proxy *httputil.ReverseProxy
}

func (r *reverseProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// not a post request
	if req.Body == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("profiling request body is nil"))
		log.Error("Incoming profiling request body is nil")
		return
	}

	bodyBytes, err := ioutil.ReadAll(http.MaxBytesReader(w, req.Body, profileMaxSize))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf("Unable to read profile body: %s", err)))
		log.Errorf("Unable to read profile body: %s", err)
		return
	}
	_ = req.Body.Close()
	req.Body = ioutil.NopCloser(bytes.NewReader(bodyBytes))

	profileID, unixNano, err := cache(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf("unable to cache profile data: %s", err)))
		log.Errorf("send profile to datakit io fail: %s", err)
		return
	}

	_ = req.Body.Close()
	req.Body = ioutil.NopCloser(bytes.NewReader(bodyBytes))

	// Retain this header to avoid fatal since Kodo still verify it.
	req.Header.Set(workspaceUUIDHeaderKey, "no-longer-in-use")
	req.Header.Set(profileIDHeaderKey, profileID)
	req.Header.Set(timestampHeaderKey, strconv.FormatInt(unixNano, 10))

	r.proxy.ServeHTTP(w, req)
}

// RegHTTPHandler simply proxy profiling request to dataway.
func (i *Input) RegHTTPHandler() {
	URL, transport, err := profilingProxyURL()
	if err != nil {
		log.Errorf("no profiling proxy url available: %s", err)
		return
	}

	httpProxy := &httputil.ReverseProxy{
		Transport: transport,

		Director: func(req *http.Request) {
			req.URL = URL
			req.Host = URL.Host // must override the host

			log.Infof("receive profiling request, bodyLength: %d, datakit will proxy the request to url [%s]",
				req.ContentLength, URL.String())

			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
		},

		ModifyResponse: func(resp *http.Response) error {
			// log proxy error
			if resp.StatusCode/100 > 2 {
				log.Errorf("profile proxy response http status: %s", resp.Status)
			} else {
				log.Infof("profile proxy response http status: %s", resp.Status)
			}
			if resp.Body != nil {
				body, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.Errorf("read profile proxy response body err: %s", err)
					return nil
				}
				if len(body) > 0 {
					_ = resp.Body.Close()
					resp.Body = ioutil.NopCloser(bytes.NewReader(body))
				}

				if resp.StatusCode/100 > 2 {
					log.Errorf("unable to upload profile binary response: %s", string(body))
				} else {
					log.Infof("upload profile binary response: %s", string(body))

					profileID := resp.Request.Header.Get(profileIDHeaderKey)

					if profileID == "" {
						return fmt.Errorf("profileID is empty")
					}

					if err := sendToIO(profileID); err != nil {
						return fmt.Errorf("unable to send profile: %w", err)
					}
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(err.Error()))
			log.Errorf("proxy error handler receive err: %s", err.Error())
		},
	}

	proxy := &reverseProxy{
		proxy: httpProxy,
	}

	for _, endpoint := range i.Endpoints {
		httpapi.RegHTTPHandler(http.MethodPost, endpoint, proxy.ServeHTTP)
		log.Infof("pattern: %s registered", endpoint)
	}
}

func (i *Input) Catalog() string {
	return inputName
}

func (i *Input) Run() {
	log = logger.SLogger(inputName)
	log.Infof("the input %s is running...", inputName)

	group := goroutine.NewGroup(goroutine.Option{
		Name: "profile",
		PanicCb: func(b []byte) bool {
			log.Error(string(b))
			return false
		},
	})

	for _, g := range i.Go {
		func(g *GoProfiler) {
			group.Go(func(ctx context.Context) error {
				if err := g.run(i); err != nil {
					log.Errorf("go profile collect error: %s", err.Error())
				}
				return nil
			})
		}(g)
	}

	for _, g := range i.PyroscopeLists {
		func(g *pyroscopeOpts) {
			group.Go(func(ctx context.Context) error {
				if err := g.run(i); err != nil {
					log.Errorf("pyroscope profile collect error: %s", err.Error())
				}
				return nil
			})
		}(g)
	}

	if err := group.Wait(); err != nil {
		log.Errorf("profile collect err: %s", err.Error())
	}
}

func (i *Input) SampleConfig() string {
	return sampleConfig
}

func (i *Input) SampleMeasurement() []inputs.Measurement {
	return []inputs.Measurement{&trace.TraceMeasurement{Name: inputName}}
}

func (i *Input) AvailableArchs() []string {
	return datakit.AllOS
}

func (i *Input) Terminate() {
	if i.semStop != nil {
		i.semStop.Close()
	}
}

type pushProfileDataOpt struct {
	startTime       time.Time
	endTime         time.Time
	profiledatas    []*profileData
	endPoint        string
	inputTags       map[string]string
	election        bool
	inputNameSuffix string
}

type eventOpts struct {
	Family   string `json:"family"`
	Format   string `json:"format"`
	Profiler string `json:"profiler"`
}

func pushProfileData(opt *pushProfileDataOpt, event *eventOpts) error {
	b := new(bytes.Buffer)
	mw := multipart.NewWriter(b)

	for _, profileData := range opt.profiledatas {
		if ff, err := mw.CreateFormFile(profileData.fileName, profileData.fileName); err != nil {
			continue
		} else {
			if _, err = io.Copy(ff, profileData.buf); err != nil {
				continue
			}
		}
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", "form-data; name=\"event\"; filename=\"event.json\"")
	h.Set("Content-Type", "application/json")
	f, err := mw.CreatePart(h)
	if err != nil {
		return err
	}

	eventJSONString, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, bytes.NewReader(eventJSONString)); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	profileID := randomProfileID()

	URL, transport, err := profilingProxyURL()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", URL.String(), b)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", mw.FormDataContentType())
	// Retain this header to avoid fatal since Kodo still verify it.
	req.Header.Set(workspaceUUIDHeaderKey, "no-longer-in-use")
	req.Header.Set(profileIDHeaderKey, profileID)
	req.Header.Set(timestampHeaderKey, strconv.FormatInt(opt.startTime.UnixNano(), 10))

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	bo, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		var resp uploadResponse

		if err := json.Unmarshal(bo, &resp); err != nil {
			return fmt.Errorf("json unmarshal upload profile binary response err: %w", err)
		}

		if resp.Content == nil || resp.Content.ProfileID == "" {
			return fmt.Errorf("fetch profile upload response profileID fail")
		}

		if err := writeProfilePoint(&writeProfilePointOpt{
			profileID:       profileID,
			startTime:       opt.startTime,
			endTime:         opt.endTime,
			reportFamily:    event.Family,
			reportFormat:    event.Format,
			endPoint:        opt.endPoint,
			inputTags:       opt.inputTags,
			election:        opt.election,
			inputNameSuffix: opt.inputNameSuffix,
		}); err != nil {
			return fmt.Errorf("write profile point failed: %w", err)
		}
	} else {
		return fmt.Errorf("push profile data failed, response status: %s", resp.Status)
	}
	return nil
}

type writeProfilePointOpt struct {
	profileID       string
	startTime       time.Time
	endTime         time.Time
	reportFamily    string
	reportFormat    string
	endPoint        string
	inputTags       map[string]string
	election        bool
	inputNameSuffix string
}

func writeProfilePoint(opt *writeProfilePointOpt) error {
	pointTags := map[string]string{
		TagEndPoint: opt.endPoint,
		TagLanguage: opt.reportFamily,
	}

	// extend custom tags
	for k, v := range opt.inputTags {
		if _, ok := pointTags[k]; !ok {
			pointTags[k] = v
		}
	}

	//nolint:lll
	pointFields := map[string]interface{}{
		FieldProfileID:  opt.profileID,
		FieldFormat:     opt.reportFormat,
		FieldDatakitVer: datakit.Version,
		FieldStart:      opt.startTime.UnixNano(),
		FieldEnd:        opt.endTime.UnixNano(),
		FieldDuration:   opt.endTime.Sub(opt.startTime).Nanoseconds(),
	}

	pt, err := point.NewPoint(inputName, pointTags, pointFields, &point.PointOption{
		Time:               opt.startTime,
		Category:           datakit.Profiling,
		Strict:             false,
		GlobalElectionTags: opt.election,
	})
	if err != nil {
		return fmt.Errorf("build profile point fail: %w", err)
	}

	err = dkio.Feed(inputName+opt.inputNameSuffix,
		datakit.Profiling,
		[]*point.Point{pt},
		&dkio.Option{CollectCost: time.Since(pt.Time())})
	if err != nil {
		return err
	}

	return nil
}

func originAddTagsSafe(originTags map[string]string, newKey, newVal string) {
	if len(newKey) > 0 && len(newVal) > 0 {
		if _, ok := originTags[newKey]; !ok {
			originTags[newKey] = newVal
		}
	}
}

const (
	pyroscopeReservedPrefix = "__"
)

func getPyroscopeTagFromLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels)-1) // exclude '__name__'.
	for k, v := range labels {
		if strings.HasPrefix(k, pyroscopeReservedPrefix) {
			continue
		}
		out[k] = v
	}
	return out
}
