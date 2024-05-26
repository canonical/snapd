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

	"github.com/ddkwork/golibrary/mylog"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.snapshots.automatic.retention"] = true
}

func validateAutomaticSnapshotsExpiration(tr RunTransaction) error {
	expirationStr := mylog.Check2(coreCfg(tr, "snapshots.automatic.retention"))

	if expirationStr != "" && expirationStr != "no" {
		dur := mylog.Check2(time.ParseDuration(expirationStr))

		if dur < time.Hour*24 {
			return fmt.Errorf("snapshots.automatic.retention must be a value greater than 24 hours, or \"no\" to disable")
		}
	}
	return nil
}
