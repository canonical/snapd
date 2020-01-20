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
		// unmount all snaps
		for _, snapSt := range snaps {
			inf, err := snapSt.CurrentInfo()
			if err != nil {
				return err
			}
			logger.Debugf("unmounting snap %s at %s", inf.InstanceName(), inf.MountDir())
			if _, err := exec.Command("umount", "-d", "-l", inf.MountDir()).CombinedOutput(); err != nil {
				return err
			}
		}

		// do not mark this task done as this makes it racy against taskrunner tear down (the next task
		// could start). Let this task finish after snapd restart when prebake mode is off.
		st.RequestRestart(state.StopSnapd)

		return &state.Retry{Reason: "mark-preseeded will be marked done when snapd is executed in normal mode"}
	}

	// normal snapd run after snapd restart (not in pre-bake mode anymore)
	return nil
}

func (m *DeviceManager) doMarkSeeded(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if m.preseed {
		return fmt.Errorf("internal error: mark-seeded task not expected in pre-seeding mode")
	}
	// TODO:UC20: update "modeenv" and remove "recovery_system" from
	// it because this information is only needed for the initial
	// seeding.

	st.Set("seed-time", time.Now())
	st.Set("seeded", true)
	// make sure we setup a fallback model/consider the next phase
	// (registration) timely
	st.EnsureBefore(0)
	return nil
}
