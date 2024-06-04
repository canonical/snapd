// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	supportedConfigurations["core.users.lockout"] = true
}

func validateFaillockSettings(tr ConfGetter) error {
	return validateBoolFlag(tr, "users.lockout")
}

func handleFaillockConfiguration(dev sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	faillock, err := coreCfg(tr, "users.lockout")
	if err != nil {
		return err
	}

	marker := filepath.Join(dirs.GlobalRootDir, "/etc/writable/account-lockout.enabled")

	switch faillock {
	case "":
		// nothing to do if unset
	case "true":
		if err := os.WriteFile(marker, nil, 0644); err != nil {
			return err
		}
	case "false":
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return err
		}
	default:
		return fmt.Errorf("unsupported users.lockout value: %q", faillock)
	}

	return nil
}
