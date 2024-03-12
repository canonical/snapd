// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"errors"
	"fmt"
	"os/exec"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func (m *DeviceManager) doMarkPreseeded(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snaps, err := snapstate.All(st)
	if err != nil {
		return err
	}

	systemKey, err := interfaces.RecordedSystemKey()
	if err != nil {
		return fmt.Errorf("cannot get recorded system key: %v", err)
	}

	if m.preseed {
		var preseeded bool
		// the "preseeded" flag on this task is set to allow skipping the logic
		// below in case this handler is retried in preseeding mode due to an
		// EnsureBefore(0) done somewhere else.
		// XXX: we should probably drop the flag from the task now that we have
		// one on the state.
		if err := t.Get("preseeded", &preseeded); err != nil && !errors.Is(err, state.ErrNoState) {
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
				// Remove early mount for the kernel snap
				if tp, _ := snapSt.Type(); tp == snap.TypeKernel {
					earlyMntPt := kernel.EarlyKernelMountDir(info.RealName, info.Revision)
					if _, err := exec.Command("umount", "-d", "-l",
						earlyMntPt).CombinedOutput(); err != nil {
						return err
					}
				}
			}

			st.Set("preseeded", preseeded)
			st.Set("preseed-system-key", systemKey)
			st.Set("preseed-time", timeNow())

			// do not mark this task done as this makes it racy against taskrunner tear down (the next task
			// could start). Let this task finish after snapd restart when preseed mode is off.
			restart.Request(st, restart.StopDaemon, nil)
		}

		return &state.Retry{Reason: "mark-preseeded will be marked done when snapd is executed in normal mode"}
	}

	// normal snapd run after snapd restart (not in preseed mode anymore)

	st.Set("seed-restart-system-key", systemKey)
	if err := m.setTimeOnce("seed-restart-time", startTime); err != nil {
		return err
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

func (s *seededSystem) sameAs(other *seededSystem) bool {
	// in theory the system labels are unique, however be extra paranoid and
	// check all model related fields too
	return s.System == other.System &&
		s.Model == other.Model &&
		s.BrandID == other.BrandID &&
		s.Revision == other.Revision
}

func (m *DeviceManager) recordSeededSystem(st *state.State, whatSeeded *seededSystem) error {
	var seeded []seededSystem
	if err := st.Get("seeded-systems", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	for _, sys := range seeded {
		if sys.sameAs(whatSeeded) {
			return nil
		}
	}
	// contrary to the usual approach of appending new entries to the list
	// like we do with modeenv, the recently seeded system is added at the
	// front, as it is not considered candidate like for the other entries,
	// but rather it describes the currently existing
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
		// XXX make this a boot method
		modeEnv, err := maybeReadModeenv()
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
	if err := t.Get("seed-system", &whatSeeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if whatSeeded != nil && deviceCtx.RunMode() {
		// record what seeded in the state only when in run mode
		whatSeeded.SeedTime = now
		if err := m.recordSeededSystem(st, whatSeeded); err != nil {
			return fmt.Errorf("cannot record the seeded system: %v", err)
		}

		// since this is the most recently seeded system, it should also be the
		// default recovery system. this is important when coming back from a
		// factory-reset.
		st.Set("default-recovery-system", DefaultRecoverySystem{
			System:          whatSeeded.System,
			Model:           whatSeeded.Model,
			BrandID:         whatSeeded.BrandID,
			Revision:        whatSeeded.Revision,
			Timestamp:       whatSeeded.Timestamp,
			TimeMadeDefault: now,
		})
	}
	st.Set("seed-time", now)
	st.Set("seeded", true)
	// avoid possibly recording the same system multiple times etc.
	t.SetStatus(state.DoneStatus)
	// make sure we setup a fallback model/consider the next phase
	// (registration) timely
	st.EnsureBefore(0)
	return nil
}
