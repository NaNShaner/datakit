// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

//go:build !windows
// +build !windows

package dialtesting

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	dt "github.com/GuanceCloud/cliutils/dialtesting"
	_ "github.com/go-ping/ping"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/plugins/inputs"
)

type dialer struct {
	task dt.Task

	ticker *time.Ticker

	initTime time.Time
	testCnt  int64
	class    string

	tags     map[string]string
	updateCh chan dt.Task

	category   string
	regionName string

	measurementInfo *inputs.MeasurementInfo

	seqNumber int64 // the number of test has been executed

	failCnt int
	done    <-chan interface{}
}

func (d *dialer) updateTask(t dt.Task) error {
	select {
	case <-d.updateCh: // if closed?
		l.Warnf("task %s closed", d.task.ID())
		return fmt.Errorf("task exited")
	default:
		d.updateCh <- t
		return nil
	}
}

func (d *dialer) stop() {
	if err := d.task.Stop(); err != nil {
		l.Warnf("stop task %s failed: %s", d.task.ID(), err.Error())
	}
}

func newDialer(t dt.Task, ts map[string]string) *dialer {
	var info *inputs.MeasurementInfo
	switch t.Class() {
	case dt.ClassHTTP:
		info = (&httpMeasurement{}).Info()
	case dt.ClassTCP:
		info = (&tcpMeasurement{}).Info()
	case dt.ClassICMP:
		info = (&icmpMeasurement{}).Info()
	case dt.ClassWebsocket:
		info = (&websocketMeasurement{}).Info()
	}

	return &dialer{
		task:            t,
		updateCh:        make(chan dt.Task),
		initTime:        time.Now(),
		tags:            ts,
		measurementInfo: info,
		class:           t.Class(),
	}
}

func (d *dialer) getSendFailCount() int {
	if d.category != "" {
		return dialWorker.getFailCount(d.category)
	}
	return 0
}

func (d *dialer) run() error {
	var maskURL string
	d.ticker = d.task.Ticker()

	taskGauge.WithLabelValues(d.regionName, d.class).Inc()

	defer func() {
		taskGauge.WithLabelValues(d.regionName, d.class).Dec()
	}()

	l.Debugf("dialer: %+#v", d)

	defer d.ticker.Stop()
	defer close(d.updateCh)

	if parts, err := url.Parse(d.task.PostURLStr()); err != nil {
		taskInvalidCounter.WithLabelValues(d.regionName, d.class, "invalid_post_url").Inc()
		return fmt.Errorf("invalid post url")
	} else {
		params := parts.Query()
		if tokens, ok := params["token"]; ok {
			// check token
			if len(tokens) >= 1 {
				maskURL = getMaskURL(parts.String())
				if isValid, err := dialWorker.sender.checkToken(tokens[0], parts.Scheme, parts.Host); err != nil {
					l.Warnf("check token error: %s", err.Error())
				} else if !isValid {
					taskInvalidCounter.WithLabelValues(d.regionName, d.class, "invalid_token").Inc()
					return fmt.Errorf("invalid token")
				}
			} else {
				taskInvalidCounter.WithLabelValues(d.regionName, d.class, "token_empty").Inc()
				return fmt.Errorf("token is required")
			}
		} else {
			taskInvalidCounter.WithLabelValues(d.regionName, d.class, "token_empty").Inc()
			return fmt.Errorf("token is required")
		}
	}

	for {
		failCount := d.getSendFailCount()
		if failCount > 0 {
			taskDatawaySendFailedGauge.WithLabelValues(d.regionName, d.class, maskURL).Set(float64(failCount))
		}

		if failCount > MaxSendFailCount {
			taskInvalidCounter.WithLabelValues(d.regionName, d.class, "exceed_max_failure_count").Inc()
			l.Warnf("dial testing %s send data failed %d times", d.task.ID(), failCount)
			return nil
		}

		l.Debugf(`dialer run %+#v, fail count: %d`, d, failCount)
		d.testCnt++

		switch d.task.Class() {
		case dt.ClassHeadless:
			return fmt.Errorf("headless task deprecated")
		default:
			startTime := time.Now()
			_ = d.task.Run() //nolint:errcheck
			taskRunCostSummary.WithLabelValues(d.regionName, d.class).Observe(float64(time.Since(startTime)) / float64(time.Second))
		}

		// dialtesting start
		err := d.feedIO()
		if err != nil {
			l.Warnf("io feed failed, %s", err.Error())
		}

		select {
		case <-datakit.Exit.Wait():
			l.Infof("dial testing %s exit", d.task.ID())
			return nil

		case <-d.done:
			l.Infof("dial testing %s exit", d.task.ID())
			return nil

		case <-d.ticker.C:

		case t := <-d.updateCh:
			d.doUpdateTask(t)

			if strings.ToLower(d.task.Status()) == dt.StatusStop {
				d.stop()
				l.Info("task %s stopped", d.task.ID())
				return nil
			}
		}
	}
}

func (d *dialer) feedIO() error {
	u, err := url.Parse(d.task.PostURLStr())
	if err != nil {
		l.Warn("get invalid url, ignored")
		return err
	}

	u.Path += datakit.Logging // `/v1/write/logging`

	urlStr := u.String()
	switch d.task.Class() {
	case dt.ClassHTTP, dt.ClassTCP, dt.ClassICMP, dt.ClassWebsocket:
		d.category = urlStr
		d.pointsFeed(urlStr)
	case dt.ClassHeadless:
		return fmt.Errorf("headless task deprecated")
	default:
		// TODO other class
	}

	return nil
}

func (d *dialer) doUpdateTask(t dt.Task) {
	if err := t.Init(); err != nil {
		l.Warn(err)
		return
	}

	if d.task.GetFrequency() != t.GetFrequency() {
		d.ticker = t.Ticker() // update ticker
	}

	d.task = t
}
