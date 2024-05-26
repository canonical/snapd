// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package daemon

import (
	"context"
	"encoding/json"
	"io"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func MockSnapshotSave(newSave func(*state.State, []string, []string, map[string]*snap.SnapshotOptions) (uint64, []string, *state.TaskSet, error)) (restore func()) {
	oldSave := snapshotSave
	snapshotSave = newSave
	return func() {
		snapshotSave = oldSave
	}
}

func MockSnapshotList(newList func(context.Context, *state.State, uint64, []string) ([]client.SnapshotSet, error)) (restore func()) {
	oldList := snapshotList
	snapshotList = newList
	return func() {
		snapshotList = oldList
	}
}

func MockSnapshotExport(newExport func(context.Context, *state.State, uint64) (*snapshotstate.SnapshotExport, error)) (restore func()) {
	oldExport := snapshotExport
	snapshotExport = newExport
	return func() {
		snapshotExport = oldExport
	}
}

func MockSnapshotCheck(newCheck func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error)) (restore func()) {
	oldCheck := snapshotCheck
	snapshotCheck = newCheck
	return func() {
		snapshotCheck = oldCheck
	}
}

func MockSnapshotRestore(newRestore func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error)) (restore func()) {
	oldRestore := snapshotRestore
	snapshotRestore = newRestore
	return func() {
		snapshotRestore = oldRestore
	}
}

func MockSnapshotForget(newForget func(*state.State, uint64, []string) ([]string, *state.TaskSet, error)) (restore func()) {
	oldForget := snapshotForget
	snapshotForget = newForget
	return func() {
		snapshotForget = oldForget
	}
}

func MockSnapshotImport(newImport func(context.Context, *state.State, io.Reader) (uint64, []string, error)) (restore func()) {
	oldImport := snapshotImport
	snapshotImport = newImport
	return func() {
		snapshotImport = oldImport
	}
}

func MustUnmarshalSnapInstruction(c *check.C, jinst string) *snapInstruction {
	var inst snapInstruction
	mylog.Check(json.Unmarshal([]byte(jinst), &inst))

	return &inst
}

func MustUnmarshalSnapshotAction(c *check.C, jact string) *snapshotAction {
	var act snapshotAction
	mylog.Check(json.Unmarshal([]byte(jact), &act))

	return &act
}

type SnapshotExportResponse = snapshotExportResponse
