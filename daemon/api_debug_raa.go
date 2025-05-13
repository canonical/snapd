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

package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
)

type raaInfo struct {
	MonitoredSnaps    map[string]monitoredSnapInfo    `json:"monitored-snaps"`
	RefreshCandidates map[string]refreshCandidateInfo `json:"refresh-candidates"`
}

type monitoredSnapInfo struct {
	Pids map[string][]int `json:"pids"`
}

type refreshCandidateInfo struct {
	Revision  snap.Revision `json:"revision"`
	Version   string        `json:"version,omitempty"`
	Channel   string        `json:"channel,omitempty"`
	Monitored bool          `json:"monitored,omitempty"`
}

// refreshCandidate is a subset of refreshCandidate defined by snapstate and
// stored in "refresh-candidates" for unmarshalling.
type refreshCandidate struct {
	Version  string         `json:"version,omitempty"`
	Channel  string         `json:"channel,omitempty"`
	SideInfo *snap.SideInfo `json:"side-info,omitempty"`
	// This is the persistent variant of "monitored-snaps" in the in-memory cache.
	Monitored bool `json:"monitored,omitempty"`
}

func getMonitoringAborts(st *state.State) (map[string]context.CancelFunc, error) {
	stored := st.Cached("monitored-snaps")
	if stored == nil {
		return nil, nil
	}
	aborts, ok := stored.(map[string]context.CancelFunc)
	if !ok {
		// NOTE: should never happen save for programmer error
		return nil, fmt.Errorf(`internal error: "monitored-snaps" should be map[string]context.CancelFunc but got %T`, stored)
	}
	return aborts, nil
}

func getRAAInfo(st *state.State) Response {
	monitoringAborts, err := getMonitoringAborts(st)
	if err != nil {
		return InternalError(err.Error())
	}

	var candidates map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &candidates); err != nil && !errors.Is(err, state.ErrNoState) {
		return InternalError(err.Error())
	}

	data := &raaInfo{
		MonitoredSnaps:    make(map[string]monitoredSnapInfo, len(monitoringAborts)),
		RefreshCandidates: make(map[string]refreshCandidateInfo, len(candidates)),
	}
	for snapName, candidate := range candidates {
		info := refreshCandidateInfo{
			Revision:  candidate.SideInfo.Revision,
			Version:   candidate.Version,
			Channel:   candidate.Channel,
			Monitored: candidate.Monitored,
		}
		data.RefreshCandidates[snapName] = info
	}
	for snapName := range monitoringAborts {
		pids, err := cgroup.PidsOfSnap(snapName)
		if err != nil {
			return InternalError(err.Error())
		}
		info := monitoredSnapInfo{
			Pids: pids,
		}
		data.MonitoredSnaps[snapName] = info
	}
	return SyncResponse(data)
}
