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

package devicestate

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func populateStateFromSeedImpl(st *state.State) ([]*state.TaskSet, error) {
	// check that the state is empty
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if seeded {
		return nil, fmt.Errorf("cannot populate state: already seeded")
	}

	// ack all initial assertions
	model, err := importAssertionsFromSeed(st)
	if err != nil {
		return nil, err
	}

	seed, err := snap.ReadSeedYaml(filepath.Join(dirs.SnapSeedDir, "seed.yaml"))
	if err != nil {
		return nil, err
	}

	var required map[string]bool
	reqSnaps := model.RequiredSnaps()
	if len(reqSnaps) > 0 {
		required = make(map[string]bool, len(reqSnaps))
		for _, snap := range reqSnaps {
			required[snap] = true
		}
	}

	tsAll := []*state.TaskSet{}
	for i, sn := range seed.Snaps {
		var flags snapstate.Flags
		if sn.DevMode {
			flags.DevMode = true
		}
		if required[sn.Name] {
			flags.Required = true
		}

		path := filepath.Join(dirs.SnapSeedDir, "snaps", sn.File)

		var sideInfo snap.SideInfo
		if sn.Unasserted {
			sideInfo.RealName = sn.Name
		} else {
			si, err := snapasserts.DeriveSideInfo(path, assertstate.DB(st))
			if err == asserts.ErrNotFound {
				return nil, fmt.Errorf("cannot find signatures with metadata for snap %q (%q)", sn.Name, path)
			}
			if err != nil {
				return nil, err
			}
			sideInfo = *si
			sideInfo.Private = sn.Private
			sideInfo.Contact = sn.Contact
		}

		ts, err := snapstate.InstallPath(st, &sideInfo, path, sn.Channel, flags)
		if i > 0 {
			ts.WaitAll(tsAll[i-1])
		}

		if err != nil {
			return nil, err
		}

		tsAll = append(tsAll, ts)
	}
	if len(tsAll) == 0 {
		return nil, nil
	}

	ts := tsAll[len(tsAll)-1]
	markSeeded := st.NewTask("mark-seeded", i18n.G("Mark system seeded"))
	markSeeded.WaitAll(ts)
	tsAll = append(tsAll, state.NewTaskSet(markSeeded))

	return tsAll, nil
}

func readAsserts(fn string, batch *assertstate.Batch) ([]*asserts.Ref, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return batch.AddStream(f)
}

func importAssertionsFromSeed(st *state.State) (*asserts.Model, error) {
	device, err := auth.Device(st)
	if err != nil {
		return nil, err
	}

	// set device,model from the model assertion
	assertSeedDir := filepath.Join(dirs.SnapSeedDir, "assertions")
	dc, err := ioutil.ReadDir(assertSeedDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read assert seed dir: %s", err)
	}

	// collect
	var modelRef *asserts.Ref
	batch := assertstate.NewBatch()
	for _, fi := range dc {
		fn := filepath.Join(assertSeedDir, fi.Name())
		refs, err := readAsserts(fn, batch)
		if err != nil {
			return nil, fmt.Errorf("cannot read assertions: %s", err)
		}
		for _, ref := range refs {
			if ref.Type == asserts.ModelType {
				if modelRef != nil && modelRef.Unique() != ref.Unique() {
					return nil, fmt.Errorf("cannot add more than one model assertion")
				}
				modelRef = ref
			}
		}
	}
	// verify we have one model assertion
	if modelRef == nil {
		return nil, fmt.Errorf("need a model assertion")
	}

	if err := batch.Commit(st); err != nil {
		return nil, err
	}

	a, err := modelRef.Resolve(assertstate.DB(st).Find)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find just added assertion %v: %v", modelRef, err)
	}
	modelAssertion := a.(*asserts.Model)

	// set device,model from the model assertion
	device.Brand = modelAssertion.BrandID()
	device.Model = modelAssertion.Model()
	if err := auth.SetDevice(st, device); err != nil {
		return nil, err
	}

	return modelAssertion, nil
}
