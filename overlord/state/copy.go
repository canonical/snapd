// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package state

import (
	"fmt"
	"os"
	"time"

	"github.com/snapcore/snapd/osutil"
)

type checkpointOnlyBackend struct {
	path string
}

func (b *checkpointOnlyBackend) Checkpoint(data []byte) error {
	return osutil.AtomicWriteFile(b.path, data, 0600, 0)
}

func (b *checkpointOnlyBackend) EnsureBefore(d time.Duration) {
	panic("cannot use EnsureBefore in checkpointOnlyBackend")
}

func (b *checkpointOnlyBackend) RequestRestart(t RestartType) {
	panic("cannot use RequestRestart in checkpointOnlyBackend")
}

// CopyState takes a state from the srcStatePath and copies all
// dataEntries to the dstPath.
func CopyState(srcStatePath, dstStatePath string, dataEntries []string) error {
	f, err := os.Open(srcStatePath)
	if err != nil {
		return fmt.Errorf("cannot open state: %s", err)
	}
	defer f.Close()

	// XXX: read only the relevant parts of the state?
	srcState, err := ReadState(nil, f)
	if err != nil {
		return err
	}
	srcState.Lock()
	defer srcState.Unlock()

	dstState := New(&checkpointOnlyBackend{path: dstStatePath})
	dstState.Lock()
	defer dstState.Unlock()
	for _, dataEntry := range dataEntries {
		var v interface{}
		if err := srcState.Get(dataEntry, &v); err != nil && err != ErrNoState {
			return err
		}
		dstState.Set(dataEntry, v)
	}

	return nil
}
