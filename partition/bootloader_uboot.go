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

const (
	bootloaderUbootDirReal = "/boot/uboot"

	// the real uboot env
	bootloaderUbootFwEnvFileReal = "uboot.env"
)

// var to make it testable
var (
	atomicWriteFile = helpers.AtomicWriteFile
)

const bootloaderNameUboot bootloaderName = "u-boot"

type uboot struct {
}

func bootloaderUbootDir() string {
	return filepath.Join(dirs.GlobalRootDir, bootloaderUbootDirReal)
}

func bootloaderUbootFwEnvFile() string {
	return filepath.Join(bootloaderUbootDir(), bootloaderUbootFwEnvFileReal)
}

// newUboot create a new Uboot bootloader object
func newUboot() bootLoader {
	if !helpers.FileExists(bootloaderUbootFwEnvFile()) {
		return nil
	}

	return &uboot{}
}

func (u *uboot) Name() bootloaderName {
	return bootloaderNameUboot
}

func (u *uboot) SetBootVar(name, value string) error {
	env, err := uenv.Open(bootloaderUbootFwEnvFile())
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
	env, err := uenv.Open(bootloaderUbootFwEnvFile())
	if err != nil {
		return "", err
	}

	return env.Get(name), nil
}

func (u *uboot) BootDir() string {
	return bootloaderUbootDir()
}
