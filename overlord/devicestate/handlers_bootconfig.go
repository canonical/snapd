// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2021 Canonical Ltd
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

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func (m *DeviceManager) doUpdateManagedBootConfig(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	devCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	if devCtx.IsClassicBoot() {
		return fmt.Errorf("cannot run update boot config task on a classic system")
	}

	var seeded bool
	err = st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		// do nothing during first boot & seeding
		return nil
	}

	if devCtx.Model().Grade() == asserts.ModelGradeUnset {
		// pre UC20 system, do nothing
		return nil
	}
	if devCtx.ForRemodeling() {
		// TODO:UC20: we may need to update the boot config when snapd
		// channel is changed during remodel
		return nil
	}

	currentData, err := CurrentGadgetData(st, devCtx)
	if err != nil {
		return fmt.Errorf("cannot obtain current gadget data: %v", err)
	}
	if currentData == nil {
		// we should be past seeding
		return fmt.Errorf("internal error: no current gadget")
	}

	cmdlineAppend, err := buildAppendedKernelCommandLine(t, currentData, devCtx)
	if err != nil {
		return fmt.Errorf("cannot build appended kernel command line: %v", err)
	}

	// TODO:UC20 update recovery boot config
	updated, err := boot.UpdateManagedBootConfigs(devCtx, currentData.RootDir, cmdlineAppend)
	if err != nil {
		return fmt.Errorf("cannot update boot config assets: %v", err)
	}

	// set this status already before returning to minimize wasteful redos
	finalStatus := state.DoneStatus
	if updated {
		t.Logf("updated boot config assets")
		// boot assets were updated, request a restart now so that the
		// situation does not end up more complicated if more updates of
		// boot assets were to be applied
		return snapstate.FinishTaskWithRestart(t, finalStatus, restart.RestartSystem, nil)
	} else {
		t.SetStatus(finalStatus)
		return nil
	}
}
