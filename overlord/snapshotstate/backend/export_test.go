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
	"archive/tar"
	"fmt"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"os"
	"os/user"
	"time"

	"github.com/snapcore/snapd/osutil/sys"
)

var (
	AddDirToZip     = addDirToZip
	TarAsUser       = tarAsUser
	PickUserWrapper = pickUserWrapper
)

func MockIsTesting(newIsTesting bool) func() {
	oldIsTesting := isTesting
	isTesting = newIsTesting
	return func() {
		isTesting = oldIsTesting
	}
}

func MockUserLookupId(newLookupId func(string) (*user.User, error)) func() {
	oldLookupId := userLookupId
	userLookupId = newLookupId
	return func() {
		userLookupId = oldLookupId
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

func MockOpen(newOpen func(string) (*Reader, error)) func() {
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

func MockBackendSnapshot(newSnapshot func(string) (*client.Snapshot, error)) func() {
	oldSnapshot := backendSnapshotFromFile
	backendSnapshotFromFile = newSnapshot
	return func() {
		backendSnapshotFromFile = oldSnapshot
	}
}

func MockCreateExportFile(filename string, exportJSON bool, withDir bool) error {
	tf, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer tf.Close()
	tw := tar.NewWriter(tf)

	for _, s := range []string{"foo", "bar", "baz"} {
		f := fmt.Sprintf("5_%s_1.0_199.zip", s)

		hdr := &tar.Header{
			Name: f,
			Mode: 0644,
			Size: int64(len(s)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(s)); err != nil {
			return err
		}
	}

	if withDir {
		hdr := &tar.Header{
			Name:     dirs.SnapshotsDir,
			Mode:     0755,
			Size:     int64(0),
			Typeflag: tar.TypeDir,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err = tw.Write([]byte("")); err != nil {
			return nil
		}
	}

	if exportJSON {
		exp := fmt.Sprintf(`{"format":1, "date":"%s"}`, time.Now().Format(time.RFC3339))
		hdr := &tar.Header{
			Name: "export.json",
			Mode: 0644,
			Size: int64(len(exp)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err = tw.Write([]byte(exp)); err != nil {
			return nil
		}
	}

	return nil
}
func MockUsersForUsernames(f func(usernames []string) ([]*user.User, error)) (restore func()) {
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
