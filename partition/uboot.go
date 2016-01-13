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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"

	"github.com/mvo5/uboot-go/uenv"
)

func ubootDir() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/uboot")
}

func ubootConfigFile() string {
	return filepath.Join(ubootDir(), "uEnv.txt")
}

func ubootFwEnvFile() string {
	return filepath.Join(ubootDir(), "uboot.env")
}

type uboot struct {
}

// newUboot create a new Uboot bootloader object
func newUboot() bootLoader {
	if !helpers.FileExists(ubootConfigFile()) {
		return nil
	}

	return &uboot{}
}

func (u *uboot) SetBootVar(name, value string) error {
	env, err := uenv.Open(ubootFwEnvFile())
	if err != nil {
		return err
	}

	// already set, nothing to do
	if env.Get(name) == value {
		return nil
	}

	env.Set(name, value)
	return env.Save()
}

func (u *uboot) GetBootVar(name string) (string, error) {
	env, err := uenv.Open(ubootFwEnvFile())
	if err != nil {
		return "", err
	}

	return env.Get(name), nil
}
