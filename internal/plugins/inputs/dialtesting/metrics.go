// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package dialtesting

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/GuanceCloud/cliutils/metrics"
)

var (
	taskGauge                  *prometheus.GaugeVec
	taskDatawaySendFailedGauge *prometheus.GaugeVec
	taskPullCostSummary        *prometheus.SummaryVec
	taskSynchronizedCounter    *prometheus.CounterVec
	taskCheckCostSummary       *prometheus.SummaryVec
	taskRunCostSummary         *prometheus.SummaryVec
	taskInvalidCounter         *prometheus.CounterVec

	workerJobChanGauge     *prometheus.GaugeVec
	workerCachePointsGauge *prometheus.GaugeVec
	workerSendPointsGauge  *prometheus.GaugeVec
)

func metricsSetup() {
	taskGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "task_number",
			Help:      "The number of tasks",
		},
		[]string{"region", "protocol"},
	)

	taskDatawaySendFailedGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "dataway_send_failed_number",
			Help:      "The number of failed sending for each dataway",
		},
		[]string{"region", "protocol", "dataway"},
	)

	taskPullCostSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "pull_cost_seconds",
			Help:      "Time cost to pull tasks",
		},
		[]string{"region", "is_first"},
	)

	taskSynchronizedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "task_synchronized_total",
			Help:      "Task synchronized number",
		},
		[]string{"region", "protocol"},
	)

	taskInvalidCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "task_invalid_total",
			Help:      "Invalid task number",
		},
		[]string{"region", "protocol", "fail_reason"},
	)

	taskCheckCostSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "task_check_cost_seconds",
			Help:      "Task check time",
		},
		[]string{"region", "protocol", "status"},
	)

	taskRunCostSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "task_run_cost_seconds",
			Help:      "Task run time",
		},
		[]string{"region", "protocol"},
	)

	workerJobChanGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "worker_job_chan_number",
			Help:      "The number of the chan for the jobs",
		},
		[]string{"type"},
	)

	workerCachePointsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "worker_cached_points_number",
			Help:      "The number of cached points",
		},
		[]string{"region", "protocol"},
	)

	workerSendPointsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "datakit",
			Subsystem: "dialtesting",
			Name:      "worker_send_points_number",
			Help:      "The number of the points which have been sent",
		},
		[]string{"region", "protocol", "status"},
	)
}

//nolint:gochecknoinits
func init() {
	metricsSetup()
	metrics.MustRegister([]prometheus.Collector{
		taskGauge,
		taskDatawaySendFailedGauge,
		taskSynchronizedCounter,
		taskPullCostSummary,
		taskCheckCostSummary,
		taskRunCostSummary,
		taskInvalidCounter,
		workerCachePointsGauge,
		workerJobChanGauge,
		workerSendPointsGauge,
	}...)
}
