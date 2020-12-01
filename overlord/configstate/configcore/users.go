// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nomanagers

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"io/ioutil"
	"os"
	"path/filepath"
)

func init() {
	supportedConfigurations["core.users.create"] = true
}

func switchHandleUsersCreate(disabled bool, opts *fsOnlyContext) error {
	autoimportCanary := dirs.SnapAssertsUsersCreateDisabledFile
	if opts != nil {
		autoimportCanary = filepath.Join(opts.RootDir, autoimportCanary)
	}

	if err := os.MkdirAll(filepath.Dir(autoimportCanary), 0755); err != nil {
		return err
	}

	if disabled {
		if err := ioutil.WriteFile(autoimportCanary, []byte("snapd autoimport user creation has been disabled\n"), 0644); err != nil {
			return err
		}
	} else {
		err := os.Remove(autoimportCanary)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func handleUsersCreateConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	output, err := coreCfg(tr, "users.create")
	if err != nil {
		return err
	}

	switch output {
	case "", "true":
		return switchHandleUsersCreate(false, opts)
	default:
		return switchHandleUsersCreate(true, opts)
	}
}
