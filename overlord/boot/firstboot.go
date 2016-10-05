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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func PopulateStateFromSeed(st *state.State) error {
	// check that the state is empty
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if seeded {
		return fmt.Errorf("cannot populate state: already seeded")
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
				return fmt.Errorf("cannot find signatures with metadata for snap %q (%q)", sn.Name, path)
			}
			if err != nil {
				return err
			}
			sideInfo = *si
			sideInfo.Private = sn.Private
		}

		ts, err := snapstate.InstallPath(st, &sideInfo, path, sn.Channel, flags)
		if i > 0 {
			ts.WaitAll(tsAll[i-1])
		}

		if err != nil {
			return err
		}

		tsAll = append(tsAll, ts)
	}
	if len(tsAll) == 0 {
		return nil
	}

	msg := fmt.Sprintf("Initialize system state")
	chg := st.NewChange("seed", msg)
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}

	// FIXME: make the last thing that runs in the "seed" change
	st.Set("seeded", true)

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
	assertSeedDir := filepath.Join(dirs.SnapSeedDir, "assertions")
	dc, err := ioutil.ReadDir(assertSeedDir)
	if os.IsNotExist(err) {
		return nil
	}
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
