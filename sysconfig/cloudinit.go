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

	"github.com/snapcore/snapd/osutil"
)

func ubuntuDataCloudDir(rootdir string) string {
	return filepath.Join(rootdir, "etc/cloud/")
}

// DisableCloudInit will disable cloud-init permanently by writing a
// cloud-init.disabled config file in etc/cloud under the target dir, which
// instructs cloud-init-generator to not trigger new cloud-init invocations.
// Note that even with this disabled file, a root user could still manually run
// cloud-init, but this capability is not provided to any strictly confined
// snap.
func DisableCloudInit(rootDir string) error {
	ubuntuDataCloud := ubuntuDataCloudDir(rootDir)
	if err := os.MkdirAll(ubuntuDataCloud, 0755); err != nil {
		return fmt.Errorf("cannot make cloud config dir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(ubuntuDataCloud, "cloud-init.disabled"), nil, 0644); err != nil {
		return fmt.Errorf("cannot disable cloud-init: %v", err)
	}

	return nil
}

func installCloudInitCfg(src, targetdir string) error {
	ccl, err := filepath.Glob(filepath.Join(src, "*.cfg"))
	if err != nil {
		return err
	}
	if len(ccl) == 0 {
		return nil
	}

	ubuntuDataCloudCfgDir := filepath.Join(ubuntuDataCloudDir(targetdir), "cloud.cfg.d/")
	if err := os.MkdirAll(ubuntuDataCloudCfgDir, 0755); err != nil {
		return fmt.Errorf("cannot make cloud config dir: %v", err)
	}

	for _, cc := range ccl {
		if err := osutil.CopyFile(cc, filepath.Join(ubuntuDataCloudCfgDir, filepath.Base(cc)), 0); err != nil {
			return err
		}
	}
	return nil
}

// TODO:UC20: - allow cloud.conf coming from the gadget
//            - think about if/what cloud-init means on "secured" models
func configureCloudInit(opts *Options) (err error) {
	if opts.TargetRootDir == "" {
		return fmt.Errorf("unable to configure cloud-init, missing target dir")
	}

	switch opts.CloudInitSrcDir {
	case "":
		// disable cloud-init by default using the writable dir
		err = DisableCloudInit(WritableDefaultsDir(opts.TargetRootDir))
	default:
		err = installCloudInitCfg(opts.CloudInitSrcDir, WritableDefaultsDir(opts.TargetRootDir))
	}
	return err
}
