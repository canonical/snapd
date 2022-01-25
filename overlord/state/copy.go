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
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/osutil"
)

type checkpointOnlyBackend struct {
	path string
}

func (b *checkpointOnlyBackend) Checkpoint(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(b.path, data, 0600, 0)
}

func (b *checkpointOnlyBackend) EnsureBefore(d time.Duration) {
	panic("cannot use EnsureBefore in checkpointOnlyBackend")
}

// copyData will copy the given subkeys specifier from srcData to dstData.
//
// The subkeys is constructed from a dotted path like "user.auth". This copy
// helper is recursive and the pos parameter tells the function the current
// position of the copy.
func copyData(subkeys []string, pos int, srcData map[string]*json.RawMessage, dstData map[string]interface{}) error {
	if pos < 0 || pos > len(subkeys) {
		return fmt.Errorf("internal error: copyData used with an out-of-bounds position: %v not in [0:%v]", pos, len(subkeys))
	}
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
		return fmt.Errorf("cannot unmarshal state entry %q with value %q as a map while trying to copy over %q", strings.Join(subkeys[:pos+1], "."), *raw, strings.Join(subkeys, "."))
	}

	// no subkey entry -> create one
	if _, ok := dstData[subkeys[pos]]; !ok {
		dstData[subkeys[pos]] = make(map[string]interface{})
	}
	// and use existing data
	var dstDatam map[string]interface{}
	switch dstDataEntry := dstData[subkeys[pos]].(type) {
	case map[string]interface{}:
		dstDatam = dstDataEntry
	case *json.RawMessage:
		dstDatam = make(map[string]interface{})
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(*dstDataEntry), &dstDatam); err != nil {
			return fmt.Errorf("internal error: cannot decode subkey %s (%q) for %v (%T)", subkeys[pos], strings.Join(subkeys, "."), dstData, dstDataEntry)
		}
	default:
		return fmt.Errorf("internal error: cannot create subkey %s (%q) for %v (%T)", subkeys[pos], strings.Join(subkeys, "."), dstData, dstData[subkeys[pos]])
	}

	return copyData(subkeys, pos+1, srcDatam, dstDatam)
}

// CopyState takes a state from the srcStatePath and copies all
// dataEntries to the dstPath. Note that srcStatePath should never
// point to a state that is in use.
func CopyState(srcStatePath, dstStatePath string, dataEntries []string) error {
	if osutil.FileExists(dstStatePath) {
		// XXX: TOCTOU - look into moving this check into
		// checkpointOnlyBackend. The issue is right now State
		// will simply panic if Commit() returns an error
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

	// No need to lock/unlock the state here, srcState should not be
	// in use at all.
	srcState, err := ReadState(nil, f)
	if err != nil {
		return err
	}

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
