// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package snapstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

func cookies(st *state.State) (map[string]string, error) {
	var snapCookies map[string]string
	if err := st.Get("snap-cookies", &snapCookies); err != nil {
		if err != state.ErrNoState {
			return nil, fmt.Errorf("cannot get snap cookies: %v", err)
		}
		snapCookies = make(map[string]string)
	}
	return snapCookies, nil
}

// SyncCookies creates snap cookies for snaps that are missing them (may be the case for snaps installed
// before the feature of running snapctl outside of hooks was introduced, leading to a warning
// from snap-confine).
// It is the caller's responsibility to lock state before calling this function.
func (m *SnapManager) SyncCookies(st *state.State) error {
	var snapNames map[string]*json.RawMessage
	if err := st.Get("snaps", &snapNames); err != nil && err != state.ErrNoState {
		return err
	}

	snapCookies, err := cookies(st)
	if err != nil {
		return err
	}

	snapsWithCookies := make(map[string]bool)
	for _, snap := range snapCookies {
		// check if we have a cookie for non-installed snap or if we have a duplicated cookie
		if _, ok := snapNames[snap]; !ok || snapsWithCookies[snap] {
			// there is no point in checking all cookies if we found a bad one - recreate them all
			snapCookies = make(map[string]string)
			snapsWithCookies = make(map[string]bool)
			break
		}
		snapsWithCookies[snap] = true
	}

	var changed bool

	// make sure every snap has a cookie, generate one if necessary
	for snap := range snapNames {
		if _, ok := snapsWithCookies[snap]; !ok {
			cookie := makeCookie()
			snapCookies[cookie] = snap
			changed = true
		}
	}

	content := make(map[string]*osutil.FileState)
	for cookie, snap := range snapCookies {
		content[fmt.Sprintf("snap.%s", snap)] = &osutil.FileState{
			Content: []byte(cookie),
			Mode:    0600,
		}
	}
	if _, _, err := osutil.EnsureDirState(dirs.SnapCookieDir, "snap.*", content); err != nil {
		return fmt.Errorf("Failed to synchronize snap cookies: %s", err)
	}

	if changed {
		st.Set("snap-cookies", &snapCookies)
	}
	return nil
}

func (m *SnapManager) createSnapCookie(st *state.State, snapName string) error {
	snapCookies, err := cookies(st)
	if err != nil {
		return err
	}

	// make sure we don't create cookie if it already exists
	for _, snap := range snapCookies {
		if snapName == snap {
			return nil
		}
	}

	cookieID, err := createCookieFile(snapName)
	if err != nil {
		return err
	}

	snapCookies[cookieID] = snapName
	st.Set("snap-cookies", &snapCookies)
	return nil
}

func (m *SnapManager) removeSnapCookie(st *state.State, snapName string) error {
	if err := removeCookieFile(snapName); err != nil {
		return err
	}

	var snapCookies map[string]string
	err := st.Get("snap-cookies", &snapCookies)
	if err != nil {
		if err != state.ErrNoState {
			return fmt.Errorf("cannot get snap cookies: %v", err)
		}
		// no cookies in the state
		return nil
	}

	for cookieID, snap := range snapCookies {
		if snapName == snap {
			delete(snapCookies, cookieID)
			st.Set("snap-cookies", snapCookies)
			return nil
		}
	}
	return nil
}

func makeCookie() string {
	return strutil.MakeRandomString(44)
}

func createCookieFile(snapName string) (cookieID string, err error) {
	cookieID = makeCookie()
	path := filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", snapName))
	err = osutil.AtomicWriteFile(path, []byte(cookieID), 0600, 0)
	if err != nil {
		return "", fmt.Errorf("Failed to create cookie file %q: %s", path, err)
	}
	return cookieID, nil
}

func removeCookieFile(snapName string) error {
	path := filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", snapName))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Failed to remove cookie file %q: %s", path, err)
	}
	return nil
}
