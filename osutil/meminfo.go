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

	"github.com/ddkwork/golibrary/mylog"
)

var procMeminfo = "/proc/meminfo"

// TotalUsableMemory returns the total usable memory in the system in bytes.
//
// Usable means (MemTotal - CmaTotal), i.e. the total amount of memory
// minus the space reserved for the CMA (Contiguous Memory Allocator).
//
// CMA memory is taken up by e.g. the framebuffer on the Raspberry Pi or
// by DSPs on specific boards.
func TotalUsableMemory() (totalMem uint64, err error) {
	f := mylog.Check2(os.Open(procMeminfo))

	defer f.Close()
	s := bufio.NewScanner(f)

	var memTotal, cmaTotal uint64
	for s.Scan() {
		var p *uint64
		l := strings.TrimSpace(s.Text())
		switch {
		case strings.HasPrefix(l, "MemTotal:"):
			p = &memTotal
		case strings.HasPrefix(l, "CmaTotal:"):
			p = &cmaTotal
		default:
			continue
		}
		fields := strings.Fields(l)
		if len(fields) != 3 || fields[2] != "kB" {
			return 0, fmt.Errorf("cannot process unexpected meminfo entry %q", l)
		}
		v := mylog.Check2(strconv.ParseUint(fields[1], 10, 64))

		*p = v * 1024
	}
	mylog.Check(s.Err())

	if memTotal == 0 {
		return 0, fmt.Errorf("cannot determine the total amount of memory in the system from %s", procMeminfo)
	}
	return memTotal - cmaTotal, nil
}

func MockProcMeminfo(newPath string) (restore func()) {
	MustBeTestBinary("mocking can only be done from tests")
	oldProcMeminfo := procMeminfo
	procMeminfo = newPath
	return func() {
		procMeminfo = oldProcMeminfo
	}
}
