// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package backend

import (
	"os"
	"os/exec"
	"os/user"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var (
	TarAsUser       = tarAsUser
	PickUserWrapper = pickUserWrapper

	IsSnapshotFilename = isSnapshotFilename

	NewMultiError = newMultiError

	AddSnapDirToZip = addSnapDirToZip
)

func MockIsTesting(newIsTesting bool) func() {
	oldIsTesting := isTesting
	isTesting = newIsTesting
	return func() {
		isTesting = oldIsTesting
	}
}

func MockOsOpen(newOsOpen func(string) (*os.File, error)) func() {
	oldOsOpen := osOpen
	osOpen = newOsOpen
	return func() {
		osOpen = oldOsOpen
	}
}

func MockDirNames(newDirNames func(*os.File, int) ([]string, error)) func() {
	oldDirNames := dirNames
	dirNames = newDirNames
	return func() {
		dirNames = oldDirNames
	}
}

func MockOpen(newOpen func(string, uint64) (*Reader, error)) func() {
	oldOpen := backendOpen
	backendOpen = newOpen
	return func() {
		backendOpen = oldOpen
	}
}

func MockSysGeteuid(newGeteuid func() sys.UserID) (restore func()) {
	oldGeteuid := sysGeteuid
	sysGeteuid = newGeteuid
	return func() {
		sysGeteuid = oldGeteuid
	}
}

func MockTarAsUser(f func(string, ...string) *exec.Cmd) (restore func()) {
	r := testutil.Backup(&tarAsUser)
	tarAsUser = f
	return r
}

func MockExecLookPath(newLookPath func(string) (string, error)) (restore func()) {
	oldLookPath := execLookPath
	execLookPath = newLookPath
	return func() {
		execLookPath = oldLookPath
	}
}

func SetUserWrapper(newUserWrapper string) (restore func()) {
	oldUserWrapper := userWrapper
	userWrapper = newUserWrapper
	return func() {
		userWrapper = oldUserWrapper
	}
}

func MockUsersForUsernames(f func(usernames []string, opts *dirs.SnapDirOptions) ([]*user.User, error)) (restore func()) {
	old := usersForUsernames
	usersForUsernames = f
	return func() {
		usersForUsernames = old
	}
}

func MockTimeNow(f func() time.Time) (restore func()) {
	oldTimeNow := timeNow
	timeNow = f
	return func() {
		timeNow = oldTimeNow
	}
}

func MockSnapshot(setID uint64, snapName string, revision snap.Revision, size int64, shaSums map[string]string) *client.Snapshot {
	return &client.Snapshot{
		SetID:    setID,
		Snap:     snapName,
		SnapID:   "id",
		Revision: revision,
		Version:  "1.0",
		Epoch:    snap.Epoch{},
		Time:     timeNow(),
		SHA3_384: shaSums,
		Size:     size,
	}
}

func MockFilepathGlob(new func(pattern string) (matches []string, err error)) (restore func()) {
	oldFilepathGlob := filepathGlob
	filepathGlob = new
	return func() {
		filepathGlob = oldFilepathGlob
	}
}

func (se *SnapshotExport) ContentHash() []byte {
	return se.contentHash
}
