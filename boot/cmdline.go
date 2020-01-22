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

package boot

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

var (
	// the kernel commandline - can be overridden in tests
	procCmdline = "/proc/cmdline"

	validModes = []string{"install", "recover", "run"}
)

func whichModeAndRecoverSystem(cmdline []byte) (mode string, sysLabel string, err error) {
	scanner := bufio.NewScanner(bytes.NewBuffer(cmdline))
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		w := scanner.Text()
		if strings.HasPrefix(w, "snapd_recovery_mode=") {
			if mode != "" {
				return "", "", fmt.Errorf("cannot specify mode more than once")
			}
			mode = strings.SplitN(w, "=", 2)[1]
			if mode == "" {
				mode = "install"
			}
			if !strutil.ListContains(validModes, mode) {
				return "", "", fmt.Errorf("cannot use unknown mode %q", mode)
			}
		}
		if strings.HasPrefix(w, "snapd_recovery_system=") {
			if sysLabel != "" {
				return "", "", fmt.Errorf("cannot specify recovery system label more than once")
			}
			sysLabel = strings.SplitN(w, "=", 2)[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	switch {
	case mode == "" && sysLabel == "":
		return "", "", fmt.Errorf("cannot detect mode nor recovery system to use")
	case mode == "install" && sysLabel == "":
		return "", "", fmt.Errorf("cannot specify install mode without system label")
	case mode == "run" && sysLabel != "":
		// XXX: should we silently ignore the label?
		sysLabel = ""
	}
	return mode, sysLabel, nil
}

// ModeAndSystemFromKernelCommandLine returns the current run mode and
// optionally the recovery system label as passed in the kernel command line by
// the bootloader
func ModeAndSystemFromKernelCommandLine() (mode, sysLabel string, err error) {
	cmdline, err := ioutil.ReadFile(procCmdline)
	if err != nil {
		return "", "", err
	}
	return whichModeAndRecoverSystem(cmdline)
}

// MockProcCmdline overrides the path to /proc/cmdline. For use in tests.
func MockProcCmdline(newPath string) (restore func()) {
	oldProcCmdline := procCmdline
	procCmdline = newPath
	return func() {
		procCmdline = oldProcCmdline
	}
}
