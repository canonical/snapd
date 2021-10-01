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
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

func (m *DeviceManager) doUpdateManagedBootConfig(t *state.Task, _ *tomb.Tomb) error {
	if release.OnClassic {
		return fmt.Errorf("cannot run update boot config task on a classic system")
	}

	st := t.State()
	st.Lock()
	defer st.Unlock()

	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if !seeded {
		// do nothing during first boot & seeding
		return nil
	}
	devCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
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

	currentData, err := currentGadgetInfo(st, devCtx)
	if err != nil {
		return fmt.Errorf("cannot obtain current gadget data: %v", err)
	}
	if currentData == nil {
		// we should be past seeding
		return fmt.Errorf("internal error: no current gadget")
	}

	// TODO:UC20 update recovery boot config
	updated, err := boot.UpdateManagedBootConfigs(devCtx, currentData.RootDir)
	if err != nil {
		return fmt.Errorf("cannot update boot config assets: %v", err)
	}
	if updated {
		t.Logf("updated boot config assets")
		// boot assets were updated, request a restart now so that the
		// situation does not end up more complicated if more updates of
		// boot assets were to be applied
		snapstate.RestartSystem(t)
	}

	// minimize wasteful redos
	t.SetStatus(state.DoneStatus)
	return nil
}
