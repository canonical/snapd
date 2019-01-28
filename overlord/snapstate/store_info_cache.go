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

package snapstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// storeInfo is information about a snap (*not* a snap revision), not
// needed in the state, that may be cached to augment the information
// returned for locally-installed snaps
type storeInfo struct {
	Media snap.MediaInfos `json:"media,omitempty"`
}

func snapStoreInfoCacheFilename(snapID string) string {
	return filepath.Join(dirs.SnapStoreInfoCacheDir, snapID) + ".json"
}

// attachStoreInfo loads the stored per-snap cache info into the given *snap.Info
func attachStoreInfo(info *snap.Info) error {
	if info.SnapID == "" {
		return nil
	}
	f, err := os.Open(snapStoreInfoCacheFilename(info.SnapID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var storeInfo storeInfo
	dec := json.NewDecoder(f)
	if err := dec.Decode(&storeInfo); err != nil {
		return fmt.Errorf("cannot decode cached store info for snap %q: %v", info.InstanceName(), err)
	}
	if dec.More() {
		return fmt.Errorf("cannot decode cached store info for snap %q: spurious content after document body", info.InstanceName())
	}

	info.Media = storeInfo.Media

	return nil
}

// cacheStoreInfo saves the given store info in the cache.
func cacheStoreInfo(snapID string, storeInfo *storeInfo) error {
	if snapID == "" {
		return nil
	}
	if err := os.MkdirAll(dirs.SnapStoreInfoCacheDir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for store snap info cache: %v", err)
	}

	af, err := osutil.NewAtomicFile(snapStoreInfoCacheFilename(snapID), 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return fmt.Errorf("cannot create file for store snap info cache: %v", err)
	}
	// on success, Cancel becomes a nop
	defer af.Cancel()

	if err := json.NewEncoder(af).Encode(storeInfo); err != nil {
		return fmt.Errorf("cannot encode store info for snap %q: %v", snapID, err)
	}

	if err := af.Commit(); err != nil {
		return fmt.Errorf("cannot commit store info cache file for snap %q: %v", snapID, err)
	}
	return nil
}

// deleteStoreInfoCache removes the cache for the given snap
func deleteStoreInfoCache(snapID string) error {
	if snapID == "" {
		return nil
	}
	if err := os.Remove(snapStoreInfoCacheFilename(snapID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove store info cache file for snap %q: %v", snapID, err)
	}
	return nil
}
