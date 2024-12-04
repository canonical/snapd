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

package cgroup

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// path where v1 controllers are listed, see
	// https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v1/index.html
	// and cgroups(7)
	cgroupV1ControllersPath = "/proc/cgroups"

	// path where v2 controllers are listed, at the root of the hierarchy tree,
	// see https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html
	cgroupV2ControllersPath = filepath.Join(cgroupMountPoint, "cgroup.controllers")

	errMemoryControllerDisabled = errors.New("cgroup memory controller is disabled on this system")
)

// CheckMemoryCgroup checks if the memory cgroup is enabled. It will return
// an error if not.
//
// Since the control groups can be enabled/disabled without the kernel config the only
// way to identify the status of memory control groups is via /proc/cgroups
// "cat /proc/cgroups | grep memory" returns the active status of memory control group
// and the 3rd parameter is the status
// 0 => false => disabled
// 1 => true => enabled
func CheckMemoryCgroup() error {
	var supp bool
	var err error
	if IsUnified() {
		supp, err = checkV2CgroupMemoryController()
	} else {
		supp, err = checkV1CgroupMemoryController()
	}

	if err != nil {
		return err
	}

	if supp {
		return nil
	}

	// no errors so far but found no evidence of memory controller to be enabled
	return errMemoryControllerDisabled
}

func checkV1CgroupMemoryController() (bool, error) {
	cgroupsFile, err := os.Open(filepath.Join(rootPath, cgroupV1ControllersPath))
	if err != nil {
		return false, fmt.Errorf("cannot open cgroups file: %w", err)
	}
	defer cgroupsFile.Close()

	scanner := bufio.NewScanner(cgroupsFile)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "memory\t") {
			memoryCgroupValues := strings.Fields(line)
			if len(memoryCgroupValues) < 4 {
				// change in size, should investigate the new structure
				return false, fmt.Errorf("cannot parse cgroups file: invalid line %q", line)
			}
			isMemoryEnabled := memoryCgroupValues[3] == "1"
			return isMemoryEnabled, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("cannot read %s contents: %w", cgroupV1ControllersPath, err)
	}

	return false, nil
}

func checkV2CgroupMemoryController() (bool, error) {
	// check at the root controller
	f, err := os.Open(filepath.Join(rootPath, cgroupV2ControllersPath))
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Split(bufio.ScanWords)
	// expecting a single line
	for scanner.Scan() {
		if ctrl := scanner.Text(); ctrl == "memory" {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, nil
}
