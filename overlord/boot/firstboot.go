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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/firstboot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

var (
	// ErrNotFirstBoot is an error that indicates that the first boot has already
	// run
	ErrNotFirstBoot = errors.New("this is not your first boot")
)

func populateStateFromSeed(st *state.State) error {
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot create state: state %q already exists", dirs.SnapStateFile)
	}

	// ack all initial assertions
	if err := importAssertionsFromSeed(st); err != nil {
		return err
	}

	seed, err := snap.ReadSeedYaml(filepath.Join(dirs.SnapSeedDir, "seed.yaml"))
	if err != nil {
		return err
	}

	tsAll := []*state.TaskSet{}
	for i, sn := range seed.Snaps {
		st.Lock()

		flags := snapstate.Flags(0)
		if sn.DevMode {
			flags |= snapstate.DevMode
		}
		path := filepath.Join(dirs.SnapSeedDir, "snaps", sn.File)

		var sideInfo snap.SideInfo
		if sn.Unasserted {
			sideInfo.RealName = sn.Name
		} else {
			si, err := snapasserts.DeriveSideInfo(path, assertstate.DB(st))
			if err == asserts.ErrNotFound {
				st.Unlock()
				return fmt.Errorf("cannot find signatures with metadata for snap %q (%q)", sn.Name, path)
			}
			if err != nil {
				st.Unlock()
				return err
			}
			sideInfo = *si
			sideInfo.Private = sn.Private
		}

		ts, err := snapstate.InstallPath(st, &sideInfo, path, sn.Channel, flags)
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

	return nil
}

func readAsserts(fn string, batch *assertstate.Batch) ([]*asserts.Ref, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return batch.AddStream(f)
}

func importAssertionsFromSeed(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	assertSeedDir := filepath.Join(dirs.SnapSeedDir, "assertions")
	dc, err := ioutil.ReadDir(assertSeedDir)
	if err != nil {
		return fmt.Errorf("cannot read assert seed dir: %s", err)
	}

	// FIXME: remove this check once asserts are mandatory
	if len(dc) == 0 {
		return nil
	}

	// collect
	var modelRef *asserts.Ref
	batch := assertstate.NewBatch()
	for _, fi := range dc {
		fn := filepath.Join(assertSeedDir, fi.Name())
		refs, err := readAsserts(fn, batch)
		if err != nil {
			return fmt.Errorf("cannot read assertions: %s", err)
		}
		for _, ref := range refs {
			if ref.Type == asserts.ModelType {
				if modelRef != nil && modelRef.Unique() != ref.Unique() {
					return fmt.Errorf("cannot add more than one model assertion")
				}
				modelRef = ref
			}
		}
	}
	// verify we have one model assertion
	if modelRef == nil {
		return fmt.Errorf("need a model assertion")
	}

	if err := batch.Commit(st); err != nil {
		return err
	}

	a, err := modelRef.Resolve(assertstate.DB(st).Find)
	if err != nil {
		return fmt.Errorf("internal error: cannot find just added assertion %v: %v", modelRef, err)
	}
	modelAssertion := a.(*asserts.Model)

	// set device,model from the model assertion
	auth.SetDevice(st, &auth.DeviceState{
		Brand: modelAssertion.BrandID(),
		Model: modelAssertion.Model(),
	})

	return nil
}

var firstbootInitialNetworkConfig = firstboot.InitialNetworkConfig

// FirstBoot will do some initial boot setup and then sync the
// state
func FirstBoot(st *state.State) error {
	// Disable firstboot on classic for now. In the long run we want
	// classic to have a firstboot as well so that we can install
	// snaps in a classic environment (LP: #1609903). However right
	// now firstboot is under heavy development so until the dust
	// settles we disable it.
	if release.OnClassic {
		return nil
	}

	if firstboot.HasRun() {
		return ErrNotFirstBoot
	}

	// FIXME: the netplan config is static, we do not need to generate
	//        it from snapd, we can just set it in e.g. ubuntu-core-config
	if err := firstbootInitialNetworkConfig(); err != nil {
		logger.Noticef("Failed during inital network configuration: %s", err)
	}

	// snappy will be in a very unhappy state if this happens,
	// because populateStateFromSeed will error if there
	// is a state file already
	if err := populateStateFromSeed(st); err != nil {
		return err
	}

	return firstboot.StampFirstBoot()
}
