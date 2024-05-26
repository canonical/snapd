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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
)

func cookies(st *state.State) (map[string]string, error) {
	var snapCookies map[string]string
	mylog.Check(st.Get("snap-cookies", &snapCookies))

	return snapCookies, nil
}

// SyncCookies creates snap cookies for snaps that are missing them (may be the case for snaps installed
// before the feature of running snapctl outside of hooks was introduced, leading to a warning
// from snap-confine).
// It is the caller's responsibility to lock state before calling this function.
func (m *SnapManager) SyncCookies(st *state.State) error {
	var instanceNames map[string]*json.RawMessage
	if mylog.Check(st.Get("snaps", &instanceNames)); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	snapCookies := mylog.Check2(cookies(st))

	snapsWithCookies := make(map[string]bool)
	for _, snap := range snapCookies {
		// check if we have a cookie for non-installed snap or if we have a duplicated cookie
		if _, ok := instanceNames[snap]; !ok || snapsWithCookies[snap] {
			// there is no point in checking all cookies if we found a bad one - recreate them all
			snapCookies = make(map[string]string)
			snapsWithCookies = make(map[string]bool)
			break
		}
		snapsWithCookies[snap] = true
	}

	var changed bool

	// make sure every snap has a cookie, generate one if necessary
	for snap := range instanceNames {
		if _, ok := snapsWithCookies[snap]; !ok {
			cookie := mylog.Check2(makeCookie())

			snapCookies[cookie] = snap
			changed = true
		}
	}

	content := make(map[string]osutil.FileState)
	for cookie, snap := range snapCookies {
		content[fmt.Sprintf("snap.%s", snap)] = &osutil.MemoryFileState{
			Content: []byte(cookie),
			Mode:    0600,
		}
	}
	_, _ := mylog.Check3(osutil.EnsureDirState(dirs.SnapCookieDir, "snap.*", content))

	if changed {
		st.Set("snap-cookies", &snapCookies)
	}
	return nil
}

func (m *SnapManager) createSnapCookie(st *state.State, instanceName string) error {
	snapCookies := mylog.Check2(cookies(st))

	// make sure we don't create cookie if it already exists
	for _, snap := range snapCookies {
		if instanceName == snap {
			return nil
		}
	}

	cookieID := mylog.Check2(createCookieFile(instanceName))

	snapCookies[cookieID] = instanceName
	st.Set("snap-cookies", &snapCookies)
	return nil
}

func (m *SnapManager) removeSnapCookie(st *state.State, instanceName string) error {
	mylog.Check(removeCookieFile(instanceName))

	var snapCookies map[string]string
	mylog.Check(st.Get("snap-cookies", &snapCookies))

	// no cookies in the state

	for cookieID, snap := range snapCookies {
		if instanceName == snap {
			delete(snapCookies, cookieID)
			st.Set("snap-cookies", snapCookies)
			return nil
		}
	}
	return nil
}

func makeCookie() (string, error) {
	return randutil.CryptoToken(39)
}

func createCookieFile(instanceName string) (cookieID string, err error) {
	cookieID = mylog.Check2(makeCookie())

	path := filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", instanceName))
	mylog.Check(osutil.AtomicWriteFile(path, []byte(cookieID), 0600, 0))

	return cookieID, nil
}

func removeCookieFile(instanceName string) error {
	path := filepath.Join(dirs.SnapCookieDir, fmt.Sprintf("snap.%s", instanceName))
	if mylog.Check(os.Remove(path)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Failed to remove cookie file %q: %s", path, err)
	}
	return nil
}
