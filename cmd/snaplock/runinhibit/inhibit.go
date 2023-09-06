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

// Package runinhibit contains operations for establishing, removing and
// querying snap run inhibition lock.
package runinhibit

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// defaultInhibitDir is the directory where inhibition files are stored.
const defaultInhibitDir = "/var/lib/snapd/inhibit"

// InhibitDir is the directory where inhibition files are stored.
// This value can be changed by calling dirs.SetRootDir.
var InhibitDir = defaultInhibitDir

func init() {
	dirs.AddRootDirCallback(func(root string) {
		InhibitDir = filepath.Join(root, defaultInhibitDir)
	})
}

// Hint is a string representing reason for the inhibition of "snap run".
type Hint string

const (
	// HintNotInhibited is used when "snap run" is not inhibited.
	HintNotInhibited Hint = ""
	// HintInhibitedGateRefresh represents inhibition of a "snap run" while gate-auto-refresh hook is run.
	HintInhibitedGateRefresh Hint = "gate-refresh"
	// HintInhibitedForRefresh represents inhibition of a "snap run" while a refresh change is being performed.
	HintInhibitedForRefresh Hint = "refresh"
	// HintInhibitedForPreDownload represents inhibition of a "snap run" while a
	// pre-download is triggering a refresh.
	HintInhibitedForPreDownload Hint = "pre-download"
)

const hintFilePostfix = "lock"

// HintFile returns the full path of the run inhibition lock file for the given snap.
func HintFile(snapName string) string {
	return filepath.Join(InhibitDir, fmt.Sprintf("%s.%s", snapName, hintFilePostfix))
}

func InhibitInfoFile(snapName string, hint Hint) string {
	return filepath.Join(InhibitDir, fmt.Sprintf("%s.%s", snapName, hint))
}

func openHintFileLock(snapName string) (*osutil.FileLock, error) {
	return osutil.NewFileLockWithMode(HintFile(snapName), 0644)
}

type InhibitInfo struct {
	Revision snap.Revision `json:"revision"`
}

func validateInhibitInfo(info InhibitInfo) error {
	if info.Revision.Unset() {
		return fmt.Errorf("snap revision cannot be unset")
	}
	return nil
}

func validateHint(hint Hint) error {
	if len(hint) == 0 {
		return fmt.Errorf("lock hint cannot be empty")
	}
	if string(hint) == hintFilePostfix {
		return fmt.Errorf("hint cannot have value %q", hintFilePostfix)
	}
	return nil
}

func removeInhibitInfoFiles(snapName string) error {
	infoGlob := filepath.Join(InhibitDir, snapName+".*")
	// There should be one file only, but just in case
	files, err := filepath.Glob(infoGlob)
	if err != nil {
		return err
	}
	for _, f := range files {
		// Don't remove hint
		if filepath.Base(f) == fmt.Sprintf("%s.%s", snapName, hintFilePostfix) {
			continue
		}
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}

// LockWithHint sets a persistent "snap run" inhibition lock, for the given snap, with a given hint.
//
// The hint cannot be empty. It should be one of the Hint constants defined in
// this package. With the hint in place "snap run" will not allow the snap to
// start and will block, presenting a user interface if possible.
func LockWithHint(snapName string, hint Hint, info InhibitInfo) error {
	if err := validateHint(hint); err != nil {
		return err
	}
	if err := validateInhibitInfo(info); err != nil {
		return err
	}
	if err := os.MkdirAll(InhibitDir, 0755); err != nil {
		return err
	}
	flock, err := openHintFileLock(snapName)
	if err != nil {
		return err
	}
	defer flock.Close()

	// The following order of execution is important to avoid race conditions.
	// Take the lock
	if err := flock.Lock(); err != nil {
		return err
	}
	// Write inhibit info
	buf, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(InhibitInfoFile(snapName, hint), buf, 0644); err != nil {
		return err
	}
	// Write hint
	f := flock.File()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err = f.WriteString(string(hint)); err != nil {
		return err
	}

	return nil
}

// Unlock truncates the run inhibition lock, for the given snap.
//
// An empty inhibition lock means uninhibited "snap run".
func Unlock(snapName string) error {
	flock, err := openHintFileLock(snapName)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer flock.Close()

	// The following order of execution is important to avoid race conditions.
	// Take the lock
	if err := flock.Lock(); err != nil {
		return err
	}
	// Write HintNotInhibited
	f := flock.File()
	if err := f.Truncate(0); err != nil {
		return err
	}
	// Remove inhibit info file
	if err := removeInhibitInfoFiles(snapName); err != nil {
		return err
	}

	return nil
}

// IsLocked returns the state of the run inhibition lock for the given snap.
//
// It returns the current, non-empty hint if inhibition is in place. Otherwise
// it returns an empty hint.
func IsLocked(snapName string) (Hint, InhibitInfo, error) {
	hintFlock, err := osutil.OpenExistingLockForReading(HintFile(snapName))
	if os.IsNotExist(err) {
		return "", InhibitInfo{}, nil
	}
	if err != nil {
		return "", InhibitInfo{}, err
	}
	defer hintFlock.Close()

	// The following order of execution is important to avoid race conditions.
	// Take the lock
	if err := hintFlock.ReadLock(); err != nil {
		return "", InhibitInfo{}, err
	}
	// Read hint
	buf, err := ioutil.ReadAll(hintFlock.File())
	if err != nil {
		return "", InhibitInfo{}, err
	}
	hint := Hint(string(buf))
	if hint == HintNotInhibited {
		return hint, InhibitInfo{}, nil
	}
	// Read inhibit info
	buf, err = ioutil.ReadFile(InhibitInfoFile(snapName, hint))
	if err != nil {
		return "", InhibitInfo{}, err
	}
	var info InhibitInfo
	err = json.Unmarshal(buf, &info)
	if err != nil {
		return "", InhibitInfo{}, err
	}

	return hint, info, nil
}

// RemoveLockFile removes the run inhibition lock for the given snap.
//
// This function should not be used as a substitute of Unlock, as race-free
// ability to inspect the inhibition state relies on flock(2) which requires the
// file to exist in the first place and non-privileged processes cannot create
// it.
//
// The function does not fail if the inhibition lock does not exist.
func RemoveLockFile(snapName string) error {
	hintFlock, err := osutil.OpenExistingLockForReading(HintFile(snapName))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer hintFlock.Close()

	// The following order of execution is important to avoid race conditions.
	// Take the lock
	if err := hintFlock.Lock(); err != nil {
		return err
	}
	// Remove inhibit info files
	if err := removeInhibitInfoFiles(snapName); err != nil {
		return err
	}
	// Remove hint file
	err = os.Remove(HintFile(snapName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
