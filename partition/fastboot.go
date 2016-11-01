// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

type fastboot struct {
	BootVars map[string]string
}

// newFastboot creates a new Fastboot bootloader object
func newFastboot() Bootloader {
	f := &fastboot{}
	if !osutil.FileExists(f.ConfigFile()) {
		return nil
	}
	return f
}

func (f *fastboot) Name() string {
	return "fastboot"
}

func (f *fastboot) Dir() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/fastboot")
}

func (f *fastboot) ConfigFile() string {
	return filepath.Join(f.Dir(), "config.yaml")
}

func (f *fastboot) GetBootVars(names ...string) (map[string]string, error) {
	return f.BootVars, nil
}

func (f *fastboot) SetBootVars(values map[string]string) error {
	for key, value := range values {
		f.BootVars[key] = value
	}
	return nil
}
