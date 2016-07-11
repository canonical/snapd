// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package boot

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/firstboot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	// ErrNotFirstBoot is an error that indicates that the first boot has already
	// run
	ErrNotFirstBoot = errors.New("this is not your first boot")
)

func populateStateFromInstalled() error {
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot create state: state %q already exists", dirs.SnapStateFile)
	}

	ovld, err := overlord.New()
	if err != nil {
		return err
	}
	st := ovld.State()

	all, err := filepath.Glob(filepath.Join(dirs.SnapSeedDir, "snaps", "*.snap"))
	if err != nil {
		return err
	}

	tsAll := []*state.TaskSet{}
	for i, snapPath := range all {
		st.Lock()

		// XXX: needing to know the name here is too early

		// everything will be sideloaded for now - that is
		// ok, we will support adding assertions soon
		snapf, err := snap.Open(snapPath)
		if err != nil {
			return err
		}
		info, err := snap.ReadInfoFromSnapFile(snapf, nil)
		if err != nil {
			return err
		}
		ts, err := snapstate.InstallPath(st, info.Name(), snapPath, "", 0)

		if i > 0 {
			ts.WaitAll(tsAll[i-1])
		}
		st.Unlock()

		if err != nil {
			return err
		}

		tsAll = append(tsAll, ts)
	}
	if len(tsAll) == 0 {
		return nil
	}

	st.Lock()
	msg := fmt.Sprintf("First boot seeding")
	chg := st.NewChange("seed", msg)
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	st.Unlock()

	// do it and wait for ready
	ovld.Loop()

	st.EnsureBefore(0)
	<-chg.Ready()

	st.Lock()
	status := chg.Status()
	err = chg.Err()
	st.Unlock()
	if status != state.DoneStatus {
		ovld.Stop()
		return fmt.Errorf("cannot run seed change: %s", err)

	}

	return ovld.Stop()
}

// FirstBoot will do some initial boot setup and then sync the
// state
func FirstBoot() error {
	if firstboot.HasRun() {
		return ErrNotFirstBoot
	}
	if err := firstboot.EnableFirstEther(); err != nil {
		logger.Noticef("Failed to bring up ethernet: %s", err)
	}

	// snappy will be in a very unhappy state if this happens,
	// because populateStateFromInstalled will error if there
	// is a state file already
	if err := populateStateFromInstalled(); err != nil {
		return err
	}

	return firstboot.StampFirstBoot()
}
