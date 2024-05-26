// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	supportedConfigurations["core.store.access"] = true
}

func validateStoreAccess(cfg ConfGetter) error {
	storeAccess := mylog.Check2(coreCfg(cfg, "store.access"))

	switch storeAccess {
	case "", "offline":
		return nil
	default:
		return errors.New("store access can only be set to 'offline'")
	}
}

// repairConfig is a set of configuration data that is consumed by the
// snap-repair command. This struct is duplicated in cmd/snap-repair.
type repairConfig struct {
	// StoreOffline is true if the store is marked as offline and should not be
	// accessed.
	StoreOffline bool `json:"store-offline"`
}

func handleStoreAccess(_ sysconfig.Device, cfg ConfGetter, opts *fsOnlyContext) error {
	access := mylog.Check2(coreCfg(cfg, "store.access"))

	data := mylog.Check2(json.Marshal(repairConfig{
		StoreOffline: access == "offline",
	}))

	configFilePath := dirs.SnapRepairConfigFile
	if opts != nil && opts.RootDir != "" {
		configFilePath = dirs.SnapRepairConfigFileUnder(opts.RootDir)
	}
	mylog.Check(os.MkdirAll(filepath.Dir(configFilePath), 0755))

	return osutil.AtomicWriteFile(configFilePath, data, 0644, 0)
}
