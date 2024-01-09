// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type readOnlyBackend struct{}

func (b *readOnlyBackend) Checkpoint(data []byte) error {
	// We do not try to write the state
	return nil
}

func (b *readOnlyBackend) EnsureBefore(d time.Duration) {
	panic("cannot use EnsureBefore in readOnlyBackend")
}

var _ state.Backend = &readOnlyBackend{}

func kernelComponentsToMount(kernelName, rootfsDir string) ([]snap.ContainerPlaceInfo, error) {
	statePath := dirs.SnapStateFileUnder(rootfsDir)
	f, err := os.Open(statePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open state file %q: %s", statePath, err)
	}
	defer f.Close()

	st, err := state.ReadState(&readOnlyBackend{}, f)
	if err != nil {
		return nil, fmt.Errorf("error reading state file %q: %s", statePath, err)
	}

	st.Lock()
	defer st.Unlock()

	var kernelst snapstate.SnapState
	if err := snapstate.Get(st, kernelName, &kernelst); err != nil {
		return nil, fmt.Errorf("error reading state for snap %q: %s", kernelName, err)
	}

	// Finally, get components
	idx := kernelst.LastIndex(kernelst.Current)
	if idx == -1 {
		return nil, fmt.Errorf("inconsistent state file, current is not in sequence")
	}
	revSideState := kernelst.Sequence.Revisions[idx]
	cpis := make([]snap.ContainerPlaceInfo, len(revSideState.Components))
	for i, comp := range revSideState.Components {
		cpis[i] = snap.MinimalComponentContainerPlaceInfo(
			comp.Component.ComponentName, comp.Revision,
			kernelName, revSideState.Snap.Revision)
	}
	return cpis, nil
}
