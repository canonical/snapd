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
	// Seeded is true when the device was seeded (same as "seeded" in the
	// state).
	Seeded bool `json:"seeded,omitempty"`

	// Preseeded is true if snap-preseed was applied.
	Preseeded bool `json:"preseeded,omitempty"`

	// PreseedStartTime is when snap-preseed was started.
	PreseedStartTime time.Time `json:"preseed-start-time,omitempty"`

	// PreseedTime is when snap-preseed work finished (i.e. before firstboot).
	PreseedTime time.Time `json:"preseed-time,omitempty"`

	// SeedStartTime is when traditional seeding started (when not preseeding).
	SeedStartTime time.Time `json:"seed-start-time,omitempty"`

	// SeedRestartTime is when seeding was started after first boot of preseeded
	// image.
	SeedRestartTime time.Time `json:"seed-restart-time,omitempty"`

	// SeedTime is when seeding finished.
	SeedTime time.Time `json:"seed-time,omitempty"`

	// PreseedSystemKey is the system-key that was created during preseeding
	// (when snap-preseed was ran).
	PreseedSystemKey interface{} `json:"preseed-system-key,omitempty"`

	// SeedRestartSystemKey is the system-key that was created on first boot of the
	// preseeded image.
	SeedRestartSystemKey interface{} `json:"seed-restart-system-key,omitempty"`
}

func getSeedingInfo(st *state.State) Response {
	var seeded, preseeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return InternalError(err.Error())
	}
	if err = st.Get("preseeded", &preseeded); err != nil && err != state.ErrNoState {
		return InternalError(err.Error())
	}

	var preseedStartTime, preseedTime, seedStartTime, seedRestartTime, seedTime time.Time
	for _, t := range []struct {
		name string
		tm   *time.Time
	}{
		{"preseed-start-time", &preseedStartTime},
		{"preseed-time", &preseedTime},
		{"seed-start-time", &seedStartTime},
		{"seed-restart-time", &seedRestartTime},
		{"seed-time", &seedTime},
	} {
		if err := st.Get(t.name, t.tm); err != nil && err != state.ErrNoState {
			return InternalError(err.Error())
		}
	}

	var preseedSysKey, seedRestartSysKey interface{}
	if err := st.Get("preseed-system-key", &preseedSysKey); err != nil && err != state.ErrNoState {
		return InternalError(err.Error())
	}
	if err := st.Get("seed-restart-system-key", &seedRestartSysKey); err != nil && err != state.ErrNoState {
		return InternalError(err.Error())
	}

	// XXX: consistency & sanity checks, e.g. if preseeded, then need to have
	// preseed-start-time, preseeded-time, preseed-system-key etc?

	data := &seedingInfo{
		Seeded:               seeded,
		Preseeded:            preseeded,
		PreseedSystemKey:     preseedSysKey,
		SeedRestartSystemKey: seedRestartSysKey,
		PreseedStartTime:     preseedStartTime,
		PreseedTime:          preseedTime,
		SeedStartTime:        seedStartTime,
		SeedRestartTime:      seedRestartTime,
		SeedTime:             seedTime,
	}

	return SyncResponse(data, nil)
}
