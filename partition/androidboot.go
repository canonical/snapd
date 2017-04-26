// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

package partition

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

type androidboot struct {
	BootVars map[string]string
}

// newAndroidboot creates a new Androidboot bootloader object
func newAndroidboot() Bootloader {
	a := &androidboot{}
	if !osutil.FileExists(a.ConfigFile()) {
		return nil
	}
	return a
}

func (a *androidboot) Name() string {
	return "androidboot"
}

func (a *androidboot) Dir() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/androidboot")
}

func (a *androidboot) ConfigFile() string {
	return filepath.Join(a.Dir(), "androidboot.env")
}

func (a *androidboot) GetBootVars(names ...string) (map[string]string, error) {
	return a.BootVars, nil
}

func (a *androidboot) SetBootVars(values map[string]string) error {
	for key, value := range values {
		a.BootVars[key] = value
	}
	return nil
}
