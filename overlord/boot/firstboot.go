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

	seed, err := snap.ReadSeedYaml(filepath.Join(dirs.SnapSeedDir, "seed.yaml"))
	if err != nil {
		return err
	}

	tsAll := []*state.TaskSet{}
	for i, sn := range seed.Snaps {
		st.Lock()

		path := filepath.Join(dirs.SnapSeedDir, "snaps", sn.File)
		ts, err := snapstate.InstallPath(st, sn.Name, path, sn.Channel, 0)
		if i > 0 {
			ts.WaitAll(tsAll[i-1])
		}
		st.Unlock()

		if err != nil {
			return err
		}

		// XXX: this is a temporary hack until we have assertions
		//      and do not need this anymore
		st.Lock()
		var ss snapstate.SnapSetup
		tasks := ts.Tasks()
		tasks[0].Get("snap-setup", &ss)
		ss.SideInfo = &snap.SideInfo{
			RealName:    sn.Name,
			SnapID:      sn.SnapID,
			Revision:    sn.Revision,
			Channel:     sn.Channel,
			DeveloperID: sn.DeveloperID,
			Developer:   sn.Developer,
			Private:     sn.Private,
		}
		tasks[0].Set("snap-setup", &ss)
		st.Unlock()

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
