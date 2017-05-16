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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition/androidbootenv"
)

type androidboot struct{}

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
	out := make(map[string]string)

	env := androidbootenv.NewEnv(a.ConfigFile())
	if err := env.Load(); err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (a *androidboot) SetBootVars(values map[string]string) error {
	env := androidbootenv.NewEnv(a.ConfigFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}
