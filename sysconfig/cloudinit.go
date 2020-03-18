// -*- Mode: Go; indent-tabs-mode: t -*-

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

package sysconfig

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

func DisableCloudInit() error {
	ubuntuDataCloud := filepath.Join(dirs.RunMnt, "ubuntu-data/system-data/etc/cloud/")
	if err := os.MkdirAll(ubuntuDataCloud, 0755); err != nil {
		return fmt.Errorf("cannot make cloud config dir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(ubuntuDataCloud, "cloud-init.disabled"), nil, 0644); err != nil {
		return fmt.Errorf("cannot disable cloud-init: %v", err)
	}

	return nil
}

func installCloudInitCfg(src string) error {
	return fmt.Errorf("installCloudInitCfg not implemented yet")
}

// disable cloud-init by default (as it's not confined)
// TODO:UC20: 1. allow drop-in cloud.cfg.d/* in mode dangerous
//            2. allow gadget cloud.cfg.d/* (with whitelisted keys?)
//            3. allow cloud.cfg.d (with whitelisted keys) for non
//               grade dangerous systems
func configureCloudInit(opts *Options) (err error) {
	switch opts.CloudInitSrcDir {
	case "":
		err = DisableCloudInit()
	default:
		err = installCloudInitCfg(opts.CloudInitSrcDir)
	}
	return err
}
