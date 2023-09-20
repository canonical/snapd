// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2022 Canonical Ltd
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

package configcore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.store.access"] = true
}

var errInvalidStoreAccess = errors.New("store access can only be set to 'offline'")

func validateStoreAccess(cfg ConfGetter) error {
	storeAccess, err := coreCfg(cfg, "store.access")
	if err != nil {
		return err
	}

	switch storeAccess {
	case "", "offline":
		return nil
	default:
		return errInvalidStoreAccess
	}
}

// repairConfig is a set of configuration data that is consumed by the
// snap-repair command. This struct is duplicated in cmd/snap-repair.
type repairConfig struct {
	// StoreOffline is true if the store is marked as offline and should not be
	// accessed.
	StoreOffline bool `json:"store-offline"`
}

func handleStoreAccess(_ sysconfig.Device, cfg ConfGetter, _ *fsOnlyContext) error {
	access, err := coreCfg(cfg, "store.access")
	if err != nil {
		return err
	}

	data, err := json.Marshal(repairConfig{
		StoreOffline: access == "offline",
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dirs.SnapRepairConfigFile), 0755); err != nil {
		return err
	}

	return osutil.AtomicWriteFile(dirs.SnapRepairConfigFile, data, 0644, 0)
}
