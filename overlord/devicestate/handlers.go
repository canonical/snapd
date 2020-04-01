// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package devicestate

import (
	"fmt"
	"os/exec"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/wrappers"
)

func (m *DeviceManager) doMarkPreseeded(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snaps, err := snapstate.All(st)
	if err != nil {
		return err
	}

	if m.preseed {
		var preseeded bool
		// the "preseeded" flag on this task is set to allow skipping the logic
		// below in case this handler is retried in preseeding mode due to an
		// EnsureBefore(0) done somewhere else.
		if err := t.Get("preseeded", &preseeded); err != nil && err != state.ErrNoState {
			return err
		}
		if !preseeded {
			preseeded = true
			t.Set("preseeded", preseeded)
			// unmount all snaps
			// TODO: move to snapstate.UnmountAllSnaps.
			for _, snapSt := range snaps {
				info, err := snapSt.CurrentInfo()
				if err != nil {
					return err
				}
				logger.Debugf("unmounting snap %s at %s", info.InstanceName(), info.MountDir())
				if _, err := exec.Command("umount", "-d", "-l", info.MountDir()).CombinedOutput(); err != nil {
					return err
				}
			}

			// do not mark this task done as this makes it racy against taskrunner tear down (the next task
			// could start). Let this task finish after snapd restart when preseed mode is off.
			st.RequestRestart(state.StopDaemon)
		}

		return &state.Retry{Reason: "mark-preseeded will be marked done when snapd is executed in normal mode"}
	}

	// normal snapd run after snapd restart (not in preseed mode anymore)

	// enable all services generated as part of preseeding, but not enabled
	// XXX: this should go away once the problem of install & services is fixed.
	for _, snapSt := range snaps {
		info, err := snapSt.CurrentInfo()
		if err != nil {
			return err
		}
		if err := wrappers.EnableSnapServices(info, progress.Null); err != nil {
			return err
		}
	}

	return nil
}

type seededSystem struct {
	// System carries the recovery system label that was used to seed the
	// current system
	System string `json:"system"`
	Model  string `json:"model"`
	// BrandID is the brand account ID
	BrandID string `json:"brand-id"`
	// Revision of the model assertion
	Revision int `json:"revision"`
	// Timestamp of model assertion
	Timestamp time.Time `json:"timestamp"`
	// SeedTime holds the timestamp when the system was seeded
	SeedTime time.Time `json:"seed-time"`
}

func (m *DeviceManager) recordSeededSystem(st *state.State, whatSeeded *seededSystem) error {
	var seeded []seededSystem
	if err := st.Get("seeded-systems", &seeded); err != nil && err != state.ErrNoState {
		return err
	}
	seeded = append([]seededSystem{*whatSeeded}, seeded...)
	st.Set("seeded-systems", seeded)
	return nil
}

func (m *DeviceManager) doMarkSeeded(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if m.preseed {
		return fmt.Errorf("internal error: mark-seeded task not expected in pre-seeding mode")
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}

	if deviceCtx.HasModeenv() && deviceCtx.RunMode() {
		modeEnv, err := m.maybeReadModeenv()
		if err != nil {
			return err
		}
		if modeEnv == nil {
			return fmt.Errorf("missing modeenv, cannot proceed")
		}
		// unset recovery_system because that is only needed during install mode
		modeEnv.RecoverySystem = ""
		err = modeEnv.Write()
		if err != nil {
			return err
		}
	}

	now := time.Now()
	var whatSeeded *seededSystem
	if err := t.Get("seed-system", &whatSeeded); err != nil && err != state.ErrNoState {
		return err
	}
	if whatSeeded != nil {
		whatSeeded.SeedTime = now
		// TODO:UC20 what about remodels?
		if err := m.recordSeededSystem(st, whatSeeded); err != nil {
			return fmt.Errorf("cannot record the seeded system: %v", err)
		}
	}
	st.Set("seed-time", now)
	st.Set("seeded", true)
	// make sure we setup a fallback model/consider the next phase
	// (registration) timely
	st.EnsureBefore(0)
	return nil
}
