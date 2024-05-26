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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ddkwork/golibrary/mylog"
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

func (hint Hint) validate() error {
	if len(hint) == 0 {
		return fmt.Errorf("lock hint cannot be empty")
	}
	if string(hint) == hintFilePostfix {
		return fmt.Errorf("hint cannot have value %q", hintFilePostfix)
	}
	return nil
}

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

// InhibitInfo holds data of the previous snap revision that will be needed by
// "snap run" while the snap is unlinked (i.e. the current symlink is removed).
type InhibitInfo struct {
	// Previous is the previous revision for the snap being refreshed.
	Previous snap.Revision `json:"previous"`
}

func (info InhibitInfo) validate() error {
	if info.Previous.Unset() {
		return fmt.Errorf("snap revision cannot be unset")
	}
	return nil
}

func removeInhibitInfoFiles(snapName string) error {
	infoGlob := filepath.Join(InhibitDir, snapName+".*")
	// There should be one file only, but just in case
	files := mylog.Check2(filepath.Glob(infoGlob))

	hintFile := filepath.Base(HintFile(snapName))
	for _, f := range files {
		// Don't remove hint
		if filepath.Base(f) == hintFile {
			continue
		}
		mylog.Check(os.Remove(f))

	}
	return nil
}

// LockWithHint sets a persistent "snap run" inhibition lock, for the given snap, with a given hint
// and saves given info that will be needed by "snap run" during inhibition (e.g. snap revision).
//
// The hint cannot be empty. It should be one of the Hint constants defined in
// this package. With the hint in place "snap run" will not allow the snap to
// start and will block, presenting a user interface if possible. Also
// info.Previous corresponding to the snap revision that was installed must be
// provided and cannot be unset.
func LockWithHint(snapName string, hint Hint, info InhibitInfo) error {
	mylog.Check(hint.validate())
	mylog.Check(info.validate())
	mylog.Check(os.MkdirAll(InhibitDir, 0755))

	flock := mylog.Check2(openHintFileLock(snapName))

	defer flock.Close()
	mylog.Check(

		// The following order of execution is important to avoid race conditions.
		// Take the lock
		flock.Lock())

	// Write inhibit info
	buf := mylog.Check2(json.Marshal(info))
	mylog.Check(os.WriteFile(InhibitInfoFile(snapName, hint), buf, 0644))

	// Write hint
	f := flock.File()
	mylog.Check(f.Truncate(0))
	mylog.Check2(f.WriteString(string(hint)))
	mylog.Check(f.Sync())

	return nil
}

// Unlock truncates the run inhibition lock, for the given snap.
//
// An empty inhibition lock means uninhibited "snap run".
func Unlock(snapName string) error {
	flock := mylog.Check2(openHintFileLock(snapName))
	if os.IsNotExist(err) {
		return nil
	}

	defer flock.Close()
	mylog.Check(

		// The following order of execution is important to avoid race conditions.
		// Take the lock
		flock.Lock())

	// Write HintNotInhibited
	f := flock.File()
	mylog.Check(f.Truncate(0))
	mylog.Check(f.Sync())
	mylog.Check(

		// Remove inhibit info file
		removeInhibitInfoFiles(snapName))

	return nil
}

// IsLocked returns the state of the run inhibition lock for the given snap.
//
// It returns the current, non-empty hint if inhibition is in place. Otherwise
// it returns an empty hint.
func IsLocked(snapName string) (Hint, InhibitInfo, error) {
	hintFlock := mylog.Check2(osutil.OpenExistingLockForReading(HintFile(snapName)))
	if os.IsNotExist(err) {
		return "", InhibitInfo{}, nil
	}

	defer hintFlock.Close()
	mylog.Check(

		// The following order of execution is important to avoid race conditions.
		// Take the lock
		hintFlock.ReadLock())

	// Read hint
	hint := mylog.Check2(hintFromFile(hintFlock.File()))

	if hint == HintNotInhibited {
		return hint, InhibitInfo{}, nil
	}
	// Read inhibit info
	info := mylog.Check2(readInhibitInfo(snapName, hint))

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
	hintFlock := mylog.Check2(osutil.OpenExistingLockForReading(HintFile(snapName)))
	if os.IsNotExist(err) {
		return nil
	}

	defer hintFlock.Close()
	mylog.Check(

		// The following order of execution is important to avoid race conditions.
		// Take the lock
		hintFlock.Lock())
	mylog.Check(

		// Remove inhibit info files
		removeInhibitInfoFiles(snapName))
	mylog.Check(

		// Remove hint file
		os.Remove(HintFile(snapName)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// needed for better mocking
type ticker interface {
	Wait() <-chan time.Time
}

type timeTicker struct {
	interval time.Duration
}

func (t *timeTicker) Wait() <-chan time.Time {
	return time.After(t.interval)
}

var newTicker = func(interval time.Duration) ticker {
	return &timeTicker{interval}
}

// WaitWhileInhibited blocks until snap is not inhibited anymore and then returns
// a locked hint file lock.
//
// The `inhibited` callback is run if the snap is initially inhibited, otherwise the `notInhibited`
// callback is run. In either case, this callback runs with the hint file lock held`.
//
// If inhibited callback returns true, WaitWhileInhibited returns instantly without
// waiting for the snap not being inhibited.
//
// NOTE: A snap without a hint file is considered not inhibited and a nil FileLock is returned.
//
// NOTE: It is the caller's responsibility to release the returned file lock.
var WaitWhileInhibited = func(ctx context.Context, snapName string, notInhibited func(ctx context.Context) error, inhibited func(ctx context.Context, hint Hint, inhibitInfo *InhibitInfo) (cont bool, err error), interval time.Duration) (flock *osutil.FileLock, err error) {
	ticker := newTicker(interval)

	// Release lock if we return early with an error
	defer func() {
		// Keep lock if no errors occur
		if err != nil && flock != nil {
			flock.Close()
			flock = nil
		}
	}()

	for {
		flock = mylog.Check2(osutil.OpenExistingLockForReading(HintFile(snapName)))
		// We must return flock alongside errors so that cleanup defer can close it.
		if os.IsNotExist(err) {
			if notInhibited != nil {
				mylog.Check(notInhibited(ctx))
				// No flock opened, it is okay to return nil here
			}
			return nil, nil
		}
		mylog.Check(

			// No flock opened, it is okay to return nil here

			// Hold read lock
			flock.ReadLock())

		// Read inhibition hint
		hint := mylog.Check2(hintFromFile(flock.File()))

		if hint == HintNotInhibited {
			if notInhibited != nil {
				mylog.Check(notInhibited(ctx))
			}
			return flock, nil
		} else {
			if inhibited != nil {
				inhibitInfo := mylog.Check2(readInhibitInfo(snapName, hint))

				cont := mylog.Check2(inhibited(ctx, hint, &inhibitInfo))

				if cont {
					return flock, nil
				}
			}
		}

		// Close the lock file explicitly so we can try to lock again after waiting for the interval.
		flock.Close()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.Wait():
		}
	}
}

func hintFromFile(hintFile *os.File) (Hint, error) {
	buf := mylog.Check2(io.ReadAll(hintFile))

	return Hint(string(buf)), nil
}

func readInhibitInfo(snapName string, hint Hint) (InhibitInfo, error) {
	buf := mylog.Check2(os.ReadFile(InhibitInfoFile(snapName, hint)))

	var info InhibitInfo
	mylog.Check(json.Unmarshal(buf, &info))

	return info, nil
}
