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

// GenerateCookies creates snap cookies for snaps that are missing them (may be the case for snaps installed
// before the feature of running snapctl outside of hooks was introduced, leading to a warning
// from snap-confine).
// It is the caller's responsibility to lock state before calling this function.
func (m *SnapManager) GenerateCookies(st *state.State) error {
	var snapNames map[string]*json.RawMessage
	if err := st.Get("snaps", &snapNames); err != nil && err != state.ErrNoState {
		return err
	}

	snapCookies, err := cookies(st)
	if err != nil {
		return err
	}

	snapWithCookies := make(map[string]bool)
	for _, snap := range snapCookies {
		snapWithCookies[snap] = true
	}

	var changed bool
	for snap := range snapNames {
		if _, ok := snapWithCookies[snap]; !ok {
			cookieID, err := createCookieFile(snap)
			if err != nil {
				return err
			}

			snapCookies[cookieID] = snap
			changed = true
		}
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
	var snapCookies map[string]string
	err := st.Get("snap-cookies", &snapCookies)
	if err != nil {
		if err != state.ErrNoState {
			return fmt.Errorf("cannot get snap cookies: %v", err)
		}
		// no cookies in the state
		if err := os.Remove(filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", snapName))); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	for cookieID, snap := range snapCookies {
		if snapName == snap {
			if err := os.Remove(filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", snapName))); err != nil && !os.IsNotExist(err) {
				return err
			}
			delete(snapCookies, cookieID)
			st.Set("snap-cookies", snapCookies)
			return nil
		}
	}
	return nil
}

func createCookieFile(snapName string) (cookieID string, err error) {
	cookieID = strutil.MakeRandomString(44)
	path := filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", snapName))
	err = osutil.AtomicWriteFile(path, []byte(cookieID), 0600, 0)
	if err != nil {
		return "", err
	}
	return cookieID, nil
}
