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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/snapcore/snapd/jsonutil"
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

func copyData(subkeys []string, pos int, srcData map[string]*json.RawMessage, dstData map[string]interface{}) error {
	raw, ok := srcData[subkeys[pos]]
	if !ok {
		return ErrNoState
	}

	if pos+1 == len(subkeys) {
		dstData[subkeys[pos]] = raw
		return nil
	}

	var srcDatam map[string]*json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &srcDatam); err != nil {
		return fmt.Errorf("option %q is not a map", strings.Join(subkeys[:pos+1], "."))
	}

	// no subkey entry -> create one
	if _, ok := dstData[subkeys[pos]]; !ok {
		dstData[subkeys[pos]] = make(map[string]interface{})
	}
	// and use existing data
	dstDatam, ok := dstData[subkeys[pos]].(map[string]interface{})
	if !ok {
		// should never happen
		return fmt.Errorf("internal error: cannot create subkey %s (%q) at for %v", subkeys[pos], strings.Join(subkeys, "."), dstData)
	}

	return copyData(subkeys, pos+1, srcDatam, dstDatam)
}

// CopyState takes a state from the srcStatePath and copies all
// dataEntries to the dstPath.
func CopyState(srcStatePath, dstStatePath string, dataEntries []string) error {
	if osutil.FileExists(dstStatePath) {
		// TOCTOU
		return fmt.Errorf("cannot copy state: %q already exists", dstStatePath)
	}
	if len(dataEntries) == 0 {
		return fmt.Errorf("cannot copy state: must provide at least one data entry to copy")
	}

	f, err := os.Open(srcStatePath)
	if err != nil {
		return fmt.Errorf("cannot open state: %s", err)
	}
	defer f.Close()

	srcState, err := ReadState(nil, f)
	if err != nil {
		return err
	}
	srcState.Lock()
	defer srcState.Unlock()

	// copy relevant data
	dstData := make(map[string]interface{})
	for _, dataEntry := range dataEntries {
		subkeys := strings.Split(dataEntry, ".")
		if err := copyData(subkeys, 0, srcState.data, dstData); err != nil && err != ErrNoState {
			return err
		}
	}

	// write it out
	dstState := New(&checkpointOnlyBackend{path: dstStatePath})
	dstState.Lock()
	defer dstState.Unlock()
	for k, v := range dstData {
		dstState.Set(k, v)
	}

	return nil
}
