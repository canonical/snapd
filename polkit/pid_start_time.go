// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2017 Canonical Ltd
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

package polkit

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
)

// getStartTimeForPid determines the start time for a given process ID
func getStartTimeForPid(pid int32) (uint64, error) {
	filename := fmt.Sprintf("/proc/%d/stat", pid)
	return getStartTimeForProcStatFile(filename)
}

// getStartTimeForProcStatFile determines the start time from a process stat file
//
// The implementation is intended to be compatible with polkit:
//
//	https://cgit.freedesktop.org/polkit/tree/src/polkit/polkitunixprocess.c
func getStartTimeForProcStatFile(filename string) (uint64, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return 0, err
	}
	contents := string(data)

	// start time is the token at index 19 after the '(process
	// name)' entry - since only this field can contain the ')'
	// character, search backwards for this to avoid malicious
	// processes trying to fool us
	//
	// See proc(5) man page for a description of the
	// /proc/[pid]/stat file format and the meaning of the
	// starttime field.
	idx := strings.IndexByte(contents, ')')
	if idx < 0 {
		return 0, fmt.Errorf("cannot parse %s", filename)
	}
	idx += 2 // skip ") "
	if idx > len(contents) {
		return 0, fmt.Errorf("cannot parse %s", filename)
	}
	tokens := strings.Split(contents[idx:], " ")
	if len(tokens) < 20 {
		return 0, fmt.Errorf("cannot parse %s", filename)
	}
	return strconv.ParseUint(tokens[19], 10, 64)
}
