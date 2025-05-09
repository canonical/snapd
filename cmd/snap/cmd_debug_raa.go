// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/snap"
)

type cmdDebugRAA struct {
	clientMixin
	unicodeMixin
}

func init() {
	addDebugCommand("raa",
		"Obtain refresh-app-awareness details",
		"obtain refresh-app-awareness details",
		func() flags.Commander {
			return &cmdDebugRAA{}
		}, nil, nil)
}

type refreshCandidateInfo struct {
	Revision  snap.Revision `json:"revision"`
	Version   string        `json:"version,omitempty"`
	Channel   string        `json:"channel,omitempty"`
	Monitored bool          `json:"monitored,omitempty"`
}

func fmtMonitored(monitored bool) string {
	if monitored {
		return "Yes"
	}
	return "No"
}

func (x *cmdDebugRAA) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		MonitoredSnaps    []string                        `json:"monitored-snaps,omitempty"`
		RefreshCandidates map[string]refreshCandidateInfo `json:"refresh-candidates,omitempty"`
	}
	if err := x.client.DebugGet("raa", &resp, nil); err != nil {
		return err
	}

	w := tabWriter()

	fmt.Fprintf(w, "Monitored snaps:\n")
	for _, snapName := range resp.MonitoredSnaps {
		fmt.Fprintf(w, "- %s\n", snapName)
	}

	fmt.Fprintf(w, "\nRefresh candidates:\n")
	fmt.Fprintf(w, "Name\tVersion\tRev\tChannel\tMonitored\n")

	for snapName, candidate := range resp.RefreshCandidates {
		// doing it this way because otherwise it's a sea of %s\t%s\t%s
		line := []string{
			snapName,
			fmtVersion(candidate.Version),
			candidate.Revision.String(),
			fmtChannel(candidate.Channel),
			fmtMonitored(candidate.Monitored),
		}
		fmt.Fprintln(w, strings.Join(line, "\t"))
	}
	w.Flush()

	return nil
}
