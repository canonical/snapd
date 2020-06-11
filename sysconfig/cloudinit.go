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
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/dirs"
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

// CloudInitState represents the various cloud-init states
type CloudInitState int

var (
	// the (?m) is needed since cloud-init output will have newlines
	cloudInitStatusRe = regexp.MustCompile(`(?m)^status: (.*)$`)

	cloudInitSnapdRestrictFile = "/etc/cloud/cloud.cfg.d/zzzz_snapd.cfg"
)

const (
	// CloudInitDisabledPermanently is when cloud-init is disabled as per the
	// cloud-init.disabled file.
	CloudInitDisabledPermanently CloudInitState = iota
	// CloudInitRestrictedBySnapd is when cloud-init has been restricted by
	// snapd with a specific config file.
	CloudInitRestrictedBySnapd
	// CloudInitUntriggered is when cloud-init is disabled because nothing has
	// triggered it to run, but it could still be run.
	CloudInitUntriggered
	// CloudInitDone is when cloud-init has been run on this boot.
	CloudInitDone
	// CloudInitRunning is when cloud-init is still running/processing and has
	// not finished yet.
	CloudInitRunning
	// CloudInitErrored is when cloud-init tried to run, but failed or had invalid
	// configuration.
	CloudInitErrored
)

func cloudInitDisabledByFile() bool {
	disabledFile := filepath.Join(dirs.GlobalRootDir, "etc/cloud/cloud-init.disabled")
	return osutil.FileExists(disabledFile)
}

// CloudInitStatus returns the current status of cloud-init.
func CloudInitStatus() (CloudInitState, error) {
	// if cloud-init has been restricted by snapd, check that first
	snapdRestrictingFile := filepath.Join(dirs.GlobalRootDir, cloudInitSnapdRestrictFile)
	if osutil.FileExists(snapdRestrictingFile) {
		return CloudInitRestrictedBySnapd, nil
	}

	out, err := exec.Command("cloud-init", "status").CombinedOutput()
	if err != nil {
		return CloudInitErrored, osutil.OutputErr(out, err)
	}
	// output should just be "status: <state>"
	match := cloudInitStatusRe.FindSubmatch(out)
	if len(match) != 2 {
		return CloudInitErrored, fmt.Errorf("invalid cloud-init output: %v", osutil.OutputErr(out, err))
	}
	switch string(match[1]) {
	case "disabled":
		// check if it was permanently disabled by the disabled file or if it
		// just "hasn't run" in which case it is untriggered
		if cloudInitDisabledByFile() {
			return CloudInitDisabledPermanently, nil
		}
		return CloudInitUntriggered, nil
	case "error":
		return CloudInitErrored, nil
	case "done":
		return CloudInitDone, nil
	case "running":
		return CloudInitRunning, nil
	default:
		// unknown what other state cloud-init could be in ...
		return CloudInitErrored, fmt.Errorf("internal error: unexpected cloud-init status %q", string(match[1]))
	}
}
