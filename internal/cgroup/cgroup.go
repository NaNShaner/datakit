// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package cgroup wraps Linux cgroup functions.
package cgroup

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/GuanceCloud/cliutils/logger"
	"github.com/shirou/gopsutil/v3/process"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/datakit"
)

var (
	cg   *Cgroup
	self *process.Process
	l    = logger.DefaultSLogger("cgroup")
)

const (
	MB = 1024 * 1024
)

type CgroupOptions struct {
	Path   string  `toml:"path"`
	CPUMax float64 `toml:"cpu_max"`
	MemMax int64   `toml:"mem_max_mb"`

	DisableOOM bool `toml:"disable_oom,omitempty"`
	Enable     bool `toml:"enable"`
}

//nolint:gochecknoinits
func init() {
	var err error
	self, err = process.NewProcess(int32(os.Getpid()))
	if err != nil {
		panic(err.Error())
	}
}

func Run(c *CgroupOptions) {
	l = logger.SLogger("cgroup")

	if c == nil || !c.Enable {
		return
	}

	cg = &Cgroup{opt: c}

	if !(0 < c.CPUMax && c.CPUMax < 100) {
		l.Errorf("CPUMax and CPUMin should be in range of (0.0, 100.0)")
		return
	}

	g := datakit.G("internal_cgroup")

	g.Go(func(ctx context.Context) error {
		cg.start()
		return nil
	})
}

func (c *Cgroup) String() string {
	if !c.opt.Enable {
		return "-"
	}

	return fmt.Sprintf("path: %s, mem: %dMB, cpu: %.2f",
		c.opt.Path, c.opt.MemMax/MB, c.opt.CPUMax)
}

func Info() string {
	if cg == nil {
		return "not ready"
	}

	switch runtime.GOOS {
	case "linux":
		if cg.err != nil {
			return cg.err.Error()
		} else {
			return cg.String()
		}

	default:
		return "-"
	}
}

func MyMemPercent() (float32, error) {
	return self.MemoryPercent()
}

func MyCPUPercent(du time.Duration) (float64, error) {
	return self.Percent(du)
}

func MyCtxSwitch() *process.NumCtxSwitchesStat {
	if x, err := self.NumCtxSwitches(); err == nil {
		return x
	} else {
		return nil
	}
}

func MyIOCountersStat() *process.IOCountersStat {
	if x, err := self.IOCounters(); err == nil {
		return x
	} else {
		return nil
	}
}
