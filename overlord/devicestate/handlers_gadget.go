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
	"os"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

func snapState(st *state.State, name string) (*snapstate.SnapState, error) {
	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	return &snapst, nil
}

func makeRollbackDir(name string) (string, error) {
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, name)

	if err := os.MkdirAll(rollbackDir, 0750); err != nil {
		return "", err
	}

	return rollbackDir, nil
}

func currentGadgetInfo(snapst *snapstate.SnapState) (*gadget.GadgetData, error) {
	currentInfo, err := snapst.CurrentInfo()
	if err != nil && err != snapstate.ErrNoCurrent {
		return nil, err
	}
	if currentInfo == nil {
		// no current yet
		return nil, nil
	}

	constraints := &gadget.ModelConstraints{
		Classic: false,
	}
	gi, err := gadget.ReadInfo(currentInfo.MountDir(), constraints)
	if err != nil {
		return nil, err
	}
	return &gadget.GadgetData{Info: gi, RootDir: currentInfo.MountDir()}, nil
}

func pendingGadgetInfo(snapsup *snapstate.SnapSetup) (*gadget.GadgetData, error) {
	info, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
	if err != nil {
		return nil, err
	}

	constraints := &gadget.ModelConstraints{
		Classic: false,
	}
	update, err := gadget.ReadInfo(info.MountDir(), constraints)
	if err != nil {
		return nil, err
	}
	return &gadget.GadgetData{Info: update, RootDir: info.MountDir()}, nil
}

func gadgetCurrentAndUpdate(st *state.State, snapsup *snapstate.SnapSetup) (current *gadget.GadgetData, update *gadget.GadgetData, err error) {
	snapst, err := snapState(st, snapsup.InstanceName())
	if err != nil {
		return nil, nil, err
	}

	currentData, err := currentGadgetInfo(snapst)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read current gadget snap details: %v", err)
	}
	if currentData == nil {
		// don't bother reading update if there is no current
		return nil, nil, nil
	}

	newData, err := pendingGadgetInfo(snapsup)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read candidate gadget snap details: %v", err)
	}

	return currentData, newData, nil
}

var (
	gadgetUpdate = gadget.Update
)

func (m *DeviceManager) doUpdateGadgetAssets(t *state.Task, _ *tomb.Tomb) error {
	if release.OnClassic {
		return fmt.Errorf("cannot run update gadget assets task on a classic system")
	}

	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return err
	}

	currentData, updateData, err := gadgetCurrentAndUpdate(t.State(), snapsup)
	if err != nil {
		return err
	}
	if currentData == nil {
		// no updates during first boot & seeding
		return nil
	}

	snapRollbackDir, err := makeRollbackDir(fmt.Sprintf("%v_%v", snapsup.InstanceName(), snapsup.SideInfo.Revision))
	if err != nil {
		return fmt.Errorf("cannot prepare update rollback directory: %v", err)
	}

	st.Unlock()
	err = gadgetUpdate(*currentData, *updateData, snapRollbackDir, nil)
	st.Lock()
	if err != nil {
		if err == gadget.ErrNoUpdate {
			// no update needed
			t.Logf("No gadget assets update needed")
			return nil
		}
		return err
	}

	t.SetStatus(state.DoneStatus)

	if err := os.RemoveAll(snapRollbackDir); err != nil && !os.IsNotExist(err) {
		logger.Noticef("failed to remove gadget update rollback directory %q: %v", snapRollbackDir, err)
	}

	// TODO: consider having the option to do this early via recovery in
	// core20, have fallback code as well there
	st.RequestRestart(state.RestartSystem)

	return nil
}
