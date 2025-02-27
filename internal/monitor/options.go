// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package monitor

import (
	"fmt"
	"strings"
	"time"
)

type APPOption func(app *monitorAPP)

func WithRefresh(r time.Duration) APPOption {
	return func(app *monitorAPP) {
		if r < time.Second {
			app.refresh = time.Second
		} else {
			app.refresh = r
		}
	}
}

func WithMaxRun(n int) APPOption {
	return func(app *monitorAPP) {
		app.maxRun = n
	}
}

func WithHost(ipaddr string) APPOption {
	return func(app *monitorAPP) {
		app.url = fmt.Sprintf("http://%s/metrics", ipaddr)
		app.isURL = fmt.Sprintf("http://%s/stats/input", ipaddr)
	}
}

func WithMaxTableWidth(w int) APPOption {
	return func(app *monitorAPP) {
		app.maxTableWidth = w
	}
}

func WithVerbose(on bool) APPOption {
	return func(app *monitorAPP) {
		app.verbose = on
	}
}

func WithOnlyInputs(str string) APPOption {
	return func(app *monitorAPP) {
		if str != "" {
			app.onlyInputs = strings.Split(str, ",")
		}
	}
}

func WithOnlyModules(str string) APPOption {
	return func(app *monitorAPP) {
		if str != "" {
			app.onlyModules = strings.Split(str, ",")
		}
	}
}
