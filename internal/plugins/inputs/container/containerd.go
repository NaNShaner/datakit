// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package container

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	v1 "github.com/containerd/cgroups/stats/v1"
	v2 "github.com/containerd/cgroups/v2/stats"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/filter"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
)

type containerdInput struct {
	ipt       *Input
	client    *containerd.Client
	k8sClient k8sClientX // container log 需要添加 pod 信息，所以存一份 k8sclient

	criClient         cri.RuntimeServiceClient
	criRuntimeVersion *cri.VersionResponse

	loggingFilter filter.Filter
	logTable      *logTable
}

func newContainerdInput(ipt *Input) (cx *containerdInput, err error) {
	conn, err := newCRIClient(ipt.ContainerdAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to new CRI-Client: %w ", err)
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	criClient := cri.NewRuntimeServiceClient(conn)

	runtimeVersion, err := getCRIRuntimeVersion(criClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get CRI-RuntimeVersion: %w", err)
	}

	client, err := containerd.New(ipt.ContainerdAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to new containerd: %w ", err)
	}

	c := &containerdInput{
		client:            client,
		criClient:         criClient,
		criRuntimeVersion: runtimeVersion,
		ipt:               ipt,
		logTable:          newLogTable(),
	}

	if err := c.createLoggingFilters(ipt.ContainerIncludeLog, ipt.ContainerExcludeLog); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *containerdInput) stop() {
	if err := c.client.Close(); err != nil {
		l.Warnf("closed contianerd, err: %s", err)
	}
}

func (c *containerdInput) gatherMetric() ([]inputs.Measurement, error) {
	obj, err := c.gatherObject()
	if err != nil {
		return nil, err
	}

	var res []inputs.Measurement

	for _, o := range obj {
		r, ok := o.(*containerdObject)
		if !ok {
			continue
		}

		// metric 不需要这三个字段
		delete(r.tags, "name")
		delete(r.fields, "age")
		delete(r.fields, "message")

		res = append(res, &containerdMetric{
			tags:   r.tags,
			fields: r.fields,
		})
	}
	return res, nil
}

func (c *containerdInput) gatherObject() ([]inputs.Measurement, error) {
	var res []inputs.Measurement

	nsList, err := c.client.NamespaceService().List(context.TODO())
	if err != nil {
		return nil, err
	}

	l.Debugf("containerd linux_namespaces: %v", nsList)

	for _, ns := range nsList {
		// ignore docker
		if ns == "moby" {
			continue
		}
		ctx := namespaces.WithNamespace(context.Background(), ns)
		cList, err := c.client.Containers(ctx)
		if err != nil {
			l.Warnf("failed to collect containers in containerd, linux_namespace: %s, skip", ns)
			continue
		}

		for _, container := range cList {
			info, err := container.Info(ctx)
			if err != nil {
				l.Warnf("failed to get containerd info, err: %s, skip", err)
				continue
			}

			l.Debugf("containerd-info: id %s, image %s, labels %v", info.ID, info.Image, info.Labels)

			if isPauseContainerd(&info) {
				continue
			}

			obj := newContainerdObject(&info)
			obj.tags["linux_namespace"] = ns
			obj.tags.append(c.ipt.Tags)

			// 使用更准确的 name
			resp, _ := c.criClient.ContainerStatus(context.Background(), &cri.ContainerStatusRequest{ContainerId: container.ID()})
			if resp != nil && resp.GetStatus() != nil && resp.GetStatus().State == cri.ContainerState_CONTAINER_EXITED {
				l.Debug("containerd-state is exited, id %s", container.ID())
				continue
			}

			//nolint
			obj.tags["container_runtime_name"] = "unknown"
			if resp != nil && resp.GetStatus() != nil && resp.GetStatus().GetMetadata() != nil {
				if n := resp.GetStatus().GetMetadata().GetName(); n != "" {
					obj.tags["container_runtime_name"] = n
				}
			}

			if n := getContainerNameForLabels(info.Labels); n != "" {
				obj.tags["container_name"] = n
			} else {
				obj.tags["container_name"] = obj.tags["container_runtime_name"]
			}

			metricsData, err := getContainerdMetricsData(ctx, container)
			if err != nil {
				l.Warnf("failed to get containerd metrics, err: %s, skip", err)
				continue
			}
			oldCPU, err := cpuContainerStats(metricsData, time.Now())
			if err != nil {
				l.Warn(err)
				continue
			}
			if !oldCPU.isRunning() {
				continue
			}

			mem, err := memoryContainerStats(metricsData)
			if err != nil {
				l.Warn(err)
				continue
			} else {
				obj.fields["mem_usage"] = mem.worksetBytes
				obj.fields["mem_limit"] = mem.limitBytes
				if mem.limitBytes > 0 {
					obj.fields["mem_used_percent"] = float64(mem.worksetBytes) / float64(mem.limitBytes) * 100
				} else {
					obj.fields["mem_used_percent"] = 0
				}
			}

			//nolint:gosec
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
			metricsData2, err := getContainerdMetricsData(ctx, container)
			if err != nil {
				l.Warnf("failed to get containerd metrics, err: %s, skip", err)
				continue
			}
			newCPU, err := cpuContainerStats(metricsData2, time.Now())
			if err != nil {
				l.Warn(err)
				continue
			}
			obj.fields["cpu_usage"] = oldCPU.calculatePercent(newCPU)

			obj.fields.mergeToMessage(obj.tags)
			res = append(res, obj)
		}
	}

	return res, nil
}

func (c *containerdInput) watchNewLogs() error {
	list, err := c.criClient.ListContainers(context.Background(), &cri.ListContainersRequest{Filter: nil})
	if err != nil {
		return fmt.Errorf("failed to get cri-ListContainers err: %w", err)
	}

	containers := list.GetContainers()

	var newIDs []string
	for _, container := range containers {
		newIDs = append(newIDs, container.GetId())
	}
	c.cleanMissingContainerLog(newIDs)

	l.Infof("containerd IDs: %v", newIDs)

	for idx := range containers {
		resp, err := c.criClient.ContainerStatus(context.Background(),
			&cri.ContainerStatusRequest{ContainerId: containers[idx].Id, Verbose: true})
		if err != nil {
			l.Warnf("failed to get cri-container response, id: %s, err: %s", containers[idx].Id, err)
			continue
		}

		status := resp.GetStatus()
		if status == nil {
			l.Warn("invalid containerd status, skip")
			continue
		}

		if status.GetState() != cri.ContainerState_CONTAINER_RUNNING {
			continue
		}

		info := c.queryContainerLogInfo(resp)
		if info == nil {
			continue
		}

		if !c.shouldPullContainerLog(info) {
			continue
		}

		if err := info.parseLogConfigs(); err != nil {
			l.Warn(err)
			continue
		}

		info.addStdout()
		info.fillTags()

		c.ipt.setLoggingExtraSourceMapToLogConfigs(info.logConfigs)
		c.ipt.setLoggingSourceMultilineMapToLogConfigs(info.logConfigs)
		c.ipt.setLoggingAutoMultilineToLogConfigs(info.logConfigs)

		c.ipt.setExtractK8sLabelAsTagsToLogConfigs(info.logConfigs, info.podLabels)
		c.ipt.setTagsToLogConfigs(info.logConfigs, info.tags)
		c.ipt.setGlobalTagsToLogConfigs(info.logConfigs)

		l.Debugf("containerd %s info: %#v", info.containerName, info)

		c.tailingLogs(info)
	}

	l.Debugf("current containerd logtable: %s", c.logTable.String())

	return nil
}

func (c *containerdInput) cleanMissingContainerLog(newIDs []string) {
	missingIDs := c.logTable.findDifferences(newIDs)
	for _, id := range missingIDs {
		l.Infof("clean log collection for container id %s", id)
		c.logTable.closeFromTable(id)
		c.logTable.removeFromTable(id)
	}
}

func (c *containerdInput) createLoggingFilters(include, exclude []string) error {
	in := splitRules(include)
	ex := splitRules(exclude)

	f, err := filter.NewIncludeExcludeFilter(in, ex)
	if err != nil {
		return err
	}

	c.loggingFilter = f
	return nil
}

func (c *containerdInput) ignoreImageForLogging(image string) (ignore bool) {
	if c.loggingFilter == nil {
		return
	}
	return !c.loggingFilter.Match(image)
}

func (c *containerdInput) shouldPullContainerLog(info *containerLogInfo) bool {
	if info.enabled() {
		return true
	}

	if c.logTable.inTable(info.id, info.logPath) {
		return false
	}

	if c.ignoreImageForLogging(info.image) {
		return false
	}

	return true
}

func getContainerdMetricsData(ctx context.Context, container containerd.Container) (interface{}, error) {
	task, err := container.Task(ctx, nil)
	if err != nil {
		return nil, err
	}

	metric, err := task.Metrics(ctx)
	if err != nil {
		return nil, err
	}

	data, err := typeurl.UnmarshalAny(metric.Data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

type containerdObject struct {
	tags   tagsType
	fields fieldsType
}

func (c *containerdObject) LineProto() (*point.Point, error) {
	return point.NewPoint(dockerContainerName, c.tags, c.fields, point.OOpt())
}

func (c *containerdObject) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{}
}

func newContainerdObject(info *containers.Container) *containerdObject {
	imageName, imageShortName, imageTag := ParseImage(info.Image)
	obj := &containerdObject{}
	obj.tags = map[string]string{
		"name":             info.ID,
		"container_id":     info.ID,
		"image":            info.Image,
		"image_name":       imageName,
		"image_short_name": imageShortName,
		"image_tag":        imageTag,
		"runtime":          info.Runtime.Name,
		"container_type":   "containerd",
	}
	obj.fields = map[string]interface{}{
		// 毫秒除以1000得秒数，不使用Second()因为它返回浮点
		"age": time.Since(info.CreatedAt).Milliseconds() / 1e3,
	}

	obj.tags.addValueIfNotEmpty("pod_name", getPodNameForLabels(info.Labels))
	obj.tags.addValueIfNotEmpty("namespace", getPodNamespaceForLabels(info.Labels))
	return obj
}

func isPauseContainerd(info *containers.Container) bool {
	_, imageShortName, _ := ParseImage(info.Image)
	// ex: pause@sha256
	return strings.Contains(imageShortName, "pause")
}

type containerdMetric struct {
	tags   tagsType
	fields fieldsType
}

func (c *containerdMetric) LineProto() (*point.Point, error) {
	return point.NewPoint(dockerContainerName, c.tags, c.fields, point.MOpt())
}

func (c *containerdMetric) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{}
}

type cpuContainerUsage struct {
	usageCoreNanoSeconds int
	timestamp            time.Time
}

func (c *cpuContainerUsage) isRunning() bool {
	return c.usageCoreNanoSeconds != 0
}

func (c *cpuContainerUsage) calculatePercent(currentUsage *cpuContainerUsage) float64 {
	nanoSeconds := currentUsage.timestamp.UnixNano() - c.timestamp.UnixNano()
	if nanoSeconds <= 0 {
		return 0
	}
	return float64(currentUsage.usageCoreNanoSeconds-c.usageCoreNanoSeconds) /
		float64(nanoSeconds) * 100
}

func cpuContainerStats(stats interface{}, currentTimestamp time.Time) (*cpuContainerUsage, error) {
	switch metrics := stats.(type) {
	case *v1.Metrics:
		if metrics.CPU != nil && metrics.CPU.Usage != nil {
			return &cpuContainerUsage{
				usageCoreNanoSeconds: int(metrics.CPU.Usage.Total),
				timestamp:            currentTimestamp,
			}, nil
		}
	case *v2.Metrics:
		if metrics.CPU != nil {
			return &cpuContainerUsage{
				// convert to nano seconds
				usageCoreNanoSeconds: int(metrics.CPU.UsageUsec * 1000),
				timestamp:            currentTimestamp,
			}, nil
		}
	default:
		return nil, fmt.Errorf("unexpected metrics type: %v", metrics)
	}

	return nil, nil
}

type memContainerUsage struct {
	limitBytes   int
	worksetBytes int
}

func memoryContainerStats(stats interface{}) (*memContainerUsage, error) {
	switch metrics := stats.(type) {
	case *v1.Metrics:
		if metrics.Memory != nil && metrics.Memory.Usage != nil {
			return &memContainerUsage{
				worksetBytes: int(getWorkingSet(metrics.Memory)),
				limitBytes:   int(getLimit(metrics.Memory)),
			}, nil
		}
	case *v2.Metrics:
		if metrics.Memory != nil {
			return &memContainerUsage{
				worksetBytes: int(getWorkingSetV2(metrics.Memory)),
				limitBytes:   int(getLimitV2(metrics.Memory)),
			}, nil
		}
	default:
		return nil, fmt.Errorf("unexpected metrics type: %v", metrics)
	}

	return nil, nil
}

// getWorkingSet calculates workingset memory from cgroup memory stats.
// The caller should make sure memory is not nil.
// workingset = usage - total_inactive_file.
func getWorkingSet(memory *v1.MemoryStat) uint64 {
	if memory.Usage == nil {
		return 0
	}
	var workingSet uint64
	if memory.TotalInactiveFile < memory.Usage.Usage {
		workingSet = memory.Usage.Usage - memory.TotalInactiveFile
	}
	return workingSet
}

// getWorkingSetV2 calculates workingset memory from cgroupv2 memory stats.
// The caller should make sure memory is not nil.
// workingset = usage - inactive_file.
func getWorkingSetV2(memory *v2.MemoryStat) uint64 {
	var workingSet uint64
	if memory.InactiveFile < memory.Usage {
		workingSet = memory.Usage - memory.InactiveFile
	}
	return workingSet
}

//nolint
func isMemoryUnlimited(v uint64) bool {
	// Size after which we consider memory to be "unlimited". This is not
	// MaxInt64 due to rounding by the kernel.
	// TODO: k8s or cadvisor should export this https://github.com/google/cadvisor/blob/2b6fbacac7598e0140b5bc8428e3bdd7d86cf5b9/metrics/prometheus.go#L1969-L1971
	const maxMemorySize = uint64(1 << 62)

	return v > maxMemorySize
}

func getLimit(memory *v1.MemoryStat) uint64 {
	if isMemoryUnlimited(memory.Usage.Limit) {
		return memory.HierarchicalMemoryLimit
	}
	return memory.Usage.Limit
}

func getLimitV2(memory *v2.MemoryStat) uint64 {
	return memory.UsageLimit
}

//nolint:gochecknoinits
func init() {
	registerMeasurement(&containerdObject{})
}
