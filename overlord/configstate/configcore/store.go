// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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
	"errors"

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

func handleStoreAccess(_ sysconfig.Device, cfg ConfGetter, _ *fsOnlyContext) error {
	storeAccess, err := coreCfg(cfg, "store.access")
	if err != nil {
		return err
	}

	// TODO: write something to disk somewhere for snap-repair to read from
	// here?
	_ = storeAccess

	return nil
}
