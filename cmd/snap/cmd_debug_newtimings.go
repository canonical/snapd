// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortTimingsHelp = i18n.G("Print sandbox features available on the system")
var longTimingsHelp = i18n.G(`
The sandbox command prints tags describing features of individual sandbox
components used by snapd on a given system.
`)

type cmdTimings struct {
	clientMixin
}

type timingJson struct {
	Level    int           `json:"level"`
	Label    string        `json:"label,omitempty"`
	Summary  string        `json:"summary,omitempty"`
	Duration time.Duration `json:"duration"`
}

type timingsRootJson struct {
	Tags          map[string]string `json:"tags,omitempty"`
	NestedTimings []*timingJson     `json:"timings,omitempty"`
	StartTime time.Time `json:"start-time"`
	StopTime time.Time `json:"stop-time"`
}

func init() {
	addDebugCommand("timings2", shortTimingsHelp, longTimingsHelp, func() flags.Commander {
		return &cmdTimings{}
	}, nil, nil)
}

func (cmd *cmdTimings) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		Timings []timingsRootJson `json:"timings"`
	}

	if err := cmd.client.Debug("timings", nil, &resp); err != nil {
		return err
	}

	//w := tabWriter()
	w := os.Stdout
	fmt.Fprintf(w, "Timings\n\n")
	for _, timing := range resp.Timings {
		dur := timing.StopTime.Sub(timing.StartTime)
		if tid, ok := timing.Tags["task-id"]; ok {
			chg, _ := timing.Tags["change-id"]
			fmt.Fprintf(w, "%s\ttask: %s, change: %s, start: %s\n", dur, tid, chg, timing.StartTime)
		}
		if mgr, ok := timing.Tags["startup"]; ok {
			fmt.Fprintf(w, "%s\tmanager startup: %s\n", dur, mgr)
		}

		for _, span := range timing.NestedTimings {
			fmt.Fprintf(w, "%s\t", span.Duration)
			indent := ""
			for i := 0; i<=span.Level; i++ {
				indent += "\t"
			}
			fmt.Fprintf(w, indent + "%s, %s\n", span.Label, span.Summary)
		}
	}
	return nil
}

