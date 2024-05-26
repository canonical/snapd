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
	"fmt"
	"os"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

var cgroupsFilePath = "/proc/cgroups"

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
	cgroupsFile := mylog.Check2(os.Open(cgroupsFilePath))

	defer cgroupsFile.Close()

	scanner := bufio.NewScanner(cgroupsFile)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "memory\t") {
			memoryCgroupValues := strings.Fields(line)
			if len(memoryCgroupValues) < 4 {
				// change in size, should investigate the new structure
				return fmt.Errorf("cannot parse cgroups file: invalid line %q", line)
			}
			isMemoryEnabled := memoryCgroupValues[3] == "1"
			if !isMemoryEnabled {
				return fmt.Errorf("memory cgroup is disabled on this system")
			}
			return nil
		}
	}
	mylog.Check(scanner.Err())

	// no errors so far but the only path here is the cgroups file without the memory line
	return fmt.Errorf("cannot find memory cgroup in %s", cgroupsFilePath)
}
