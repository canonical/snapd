// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2022 Canonical Ltd
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

// auxStoreInfo is information about a snap (*not* a snap revision), not
// needed in the state, that may be stored to augment the information
// returned for locally-installed snaps
type auxStoreInfo struct {
	Media    snap.MediaInfos `json:"media,omitempty"`
	StoreURL string          `json:"store-url,omitempty"`
	// XXX this is now included in snap.SideInfo.EditedLinks but
	// continue having this to support old snapd
	Website string `json:"website,omitempty"`
}

func auxStoreInfoFilename(snapID string) string {
	return filepath.Join(dirs.SnapAuxStoreInfoDir, snapID) + ".json"
}

// retrieveAuxStoreInfo loads the stored per-snap auxiliary store info into the given *snap.Info
func retrieveAuxStoreInfo(info *snap.Info) error {
	if info.SnapID == "" {
		return nil
	}
	f, err := os.Open(auxStoreInfoFilename(info.SnapID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var aux auxStoreInfo
	dec := json.NewDecoder(f)
	if err := dec.Decode(&aux); err != nil {
		return fmt.Errorf("cannot decode auxiliary store info for snap %q: %v", info.InstanceName(), err)
	}
	if dec.More() {
		return fmt.Errorf("cannot decode auxiliary store info for snap %q: spurious content after document body", info.InstanceName())
	}

	info.Media = aux.Media
	if len(info.EditedLinks) == 0 {
		// XXX we set this to use old snapd info if it's all we have
		info.LegacyWebsite = aux.Website
	}
	info.StoreURL = aux.StoreURL

	return nil
}

// keepAuxStoreInfo saves the given auxiliary store info to disk.
func keepAuxStoreInfo(snapID string, aux *auxStoreInfo) error {
	if snapID == "" {
		return nil
	}
	if err := os.MkdirAll(dirs.SnapAuxStoreInfoDir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for auxiliary store info: %v", err)
	}

	af, err := osutil.NewAtomicFile(auxStoreInfoFilename(snapID), 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return fmt.Errorf("cannot create file for auxiliary store info for snap %s: %v", snapID, err)
	}
	// on success, Cancel becomes a nop
	defer af.Cancel()

	if err := json.NewEncoder(af).Encode(aux); err != nil {
		return fmt.Errorf("cannot encode auxiliary store info for snap %s: %v", snapID, err)
	}

	if err := af.Commit(); err != nil {
		return fmt.Errorf("cannot commit auxiliary store info file for snap %s: %v", snapID, err)
	}
	return nil
}

// discardAuxStoreInfo removes the auxiliary store info for the given snap from disk.
func discardAuxStoreInfo(snapID string) error {
	if snapID == "" {
		return nil
	}
	if err := os.Remove(auxStoreInfoFilename(snapID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove auxiliary store info file for snap %s: %v", snapID, err)
	}
	return nil
}
