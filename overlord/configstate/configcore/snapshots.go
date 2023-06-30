// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"time"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.snapshots.automatic.retention"] = true
}

func validateAutomaticSnapshotsExpiration(tr RunTransaction) error {
	expirationStr, err := coreCfg(tr, "snapshots.automatic.retention")
	if err != nil {
		return err
	}
	if expirationStr != "" && expirationStr != "no" {
		dur, err := time.ParseDuration(expirationStr)
		if err != nil {
			return fmt.Errorf("snapshots.automatic.retention cannot be parsed: %v", err)
		}
		if dur < time.Hour*24 {
			return fmt.Errorf("snapshots.automatic.retention must be a value greater than 24 hours, or \"no\" to disable")
		}
	}
	return nil
}
