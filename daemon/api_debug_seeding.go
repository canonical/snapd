// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

type seedingInfo struct {
	// true if the device is seeded (same as "seeded" in the state)
	Seeded bool `json:"seeded,omitempty"`

	// true if snap-preseed was applied
	Preseeded bool `json:"preseeded,omitempty"`

	// when snap-preseed was started
	PreseedStartTime time.Time `json:"preseed-start-time,omitempty"`

	// when snap-preseed work finished (i.e. before firstboot)
	PreseededTime time.Time `json:"preseeded-time,omitempty"`

	// when traditional seeding started (when not preseeding)
	SeedStartTime time.Time `json:"seed-start-time,omitempty"`

	// when seeding was started after first boot of preseeded image
	SeedRestartTime time.Time `json:"seed-restart-time,omitempty"`

	// when seeding finished
	SeedTime time.Time `json:"seed-time,omitempty"`

	// system-key created during preseeding (when snap-preseed was ran)
	PreseedSystemKey interface{} `json:"preseed-system-key,omitempty"`

	// system-key created on first boot of the preseeded image
	RestartSystemKey interface{} `json:"restart-system-key,omitempty"`
}

func getSeedingInfo(st *state.State) Response {
	var seeded, preseeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return BadRequest(err.Error())
	}
	if err = st.Get("preseeded", &preseeded); err != nil && err != state.ErrNoState {
		return BadRequest(err.Error())
	}

	var preseedStartTime, preseededTime, seedStartTime, seedRestartTime, seedTime time.Time
	for _, t := range []struct {
		name string
		tm   *time.Time
	}{
		{"preseed-start-time", &preseedStartTime},
		{"preseeded-time", &preseededTime},
		{"seed-start-time", &seedStartTime},
		{"seed-restart-time", &seedRestartTime},
		{"seed-time", &seedTime},
	} {
		if err := st.Get(t.name, t.tm); err != nil && err != state.ErrNoState {
			return BadRequest(err.Error())
		}
	}

	var preseedSysKey, restartSysKey interface{}
	if err := st.Get("preseed-system-key", &preseedSysKey); err != nil && err != state.ErrNoState {
		return BadRequest(err.Error())
	}
	if err := st.Get("restart-system-key", &restartSysKey); err != nil && err != state.ErrNoState {
		return BadRequest(err.Error())
	}

	// XXX: consistency & sanity checks, e.g. if preseeded, then need to have
	// preseed-start-time, preseeded-time, preseed-system-key etc?

	data := &seedingInfo{
		Seeded:           seeded,
		Preseeded:        preseeded,
		PreseedSystemKey: preseedSysKey,
		RestartSystemKey: restartSysKey,
		PreseedStartTime: preseedStartTime,
		PreseededTime:    preseededTime,
		SeedStartTime:    seedStartTime,
		SeedRestartTime:  seedRestartTime,
		SeedTime:         seedTime,
	}

	return SyncResponse(data, nil)
}
