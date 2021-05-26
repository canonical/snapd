// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package osutil

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var (
	procMeminfo = "/proc/meminfo"
)

// TotalSystemMemory returns the total memory in the system in bytes.
func TotalSystemMemory() (totalMem uint64, err error) {
	f, err := os.Open(procMeminfo)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for {
		if !s.Scan() {
			break
		}
		l := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(l, "MemTotal:") {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) != 3 || fields[2] != "kB" {
			return 0, fmt.Errorf("cannot process unexpected meminfo entry %q", l)
		}
		totalMem, err = strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot convert memory size value: %v", err)
		}
		// got it
		return totalMem * 1024, nil
	}
	if err := s.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("cannot determine the total amount of memory in the system from %s", procMeminfo)
}

func MockProcMeminfo(newPath string) (restore func()) {
	MustBeTestBinary("mocking can only be done from tests")
	oldProcMeminfo := procMeminfo
	procMeminfo = newPath
	return func() {
		procMeminfo = oldProcMeminfo
	}
}
