// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package hotplug

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

var udevadmBin = `udevadm`

func parseUdevadmOutput(cmd *exec.Cmd, rd *bufio.Scanner, devices chan<- *HotplugDeviceInfo, parseErrors chan<- error) {
	defer close(devices)
	defer close(parseErrors)

	env := make(map[string]string)

	var deviceBlock bool
	for rd.Scan() {
		// udevadm export output is divided in per-device blocks, blocks are separated
		// with empty lines, each block starts with device path (P:) line, within a block
		// there is one attribute per line, example:
		//
		// P: /devices/virtual/workqueue/nvme-wq
		// E: DEVPATH=/devices/virtual/workqueue/nvme-wq
		// E: SUBSYSTEM=workqueue
		// <empty-line>
		// P: /devices/virtual/block/dm-1
		// N: dm-1
		// S: disk/by-id/dm-name-linux-root
		// E: DEVNAME=/dev/dm-1
		// E: USEC_INITIALIZED=8899394
		// <empty-line>
		line := rd.Text()
		if line == "" {
			deviceBlock = false
			if len(env) > 0 {
				outputDevice(env, devices, parseErrors)
				env = make(map[string]string)
			}
			continue
		}
		if strings.HasPrefix(line, "P: ") {
			deviceBlock = true
			continue
		}

		// any property we find needs to belong to a device "block" started by "P: "
		if deviceBlock == false {
			parseErrors <- fmt.Errorf("no device block marker found before %q", line)
			continue
		}

		// We are only interested in 'E' properties as they carry all the interesting data,
		// including DEVPATH and DEVNAME which seem to mirror the 'P' and 'N' values.
		if strings.HasPrefix(line, "E: ") {
			if kv := strings.SplitN(line[3:], "=", 2); len(kv) == 2 {
				env[kv[0]] = kv[1]
			} else {
				parseErrors <- fmt.Errorf("failed to parse udevadm output %q", line)
			}
		}
	}

	if err := rd.Err(); err != nil {
		parseErrors <- fmt.Errorf("failed to read udevadm output: %s", err)
		env = nil
	}
	if err := cmd.Wait(); err != nil {
		env = nil
		parseErrors <- fmt.Errorf("failed to read udevadm output: %s", err)
	}

	// eof, flush remaining device
	if len(env) > 0 {
		outputDevice(env, devices, parseErrors)
	}
}

// EnumerateExistingDevices all devices by parsing 'udevadm info -e' command output and reports them
// via devices channel. The devices channel gets closed to indicate that all devices were processed.
// Parsing errors are reported to parseErrors channel. Fatal errors encountered when starting udevadm
// are reported by the error return value.
func EnumerateExistingDevices(devices chan<- *HotplugDeviceInfo, parseErrors chan<- error) error {
	cmd := exec.Command(udevadmBin, "info", "-e")
	stdout, err := cmd.StdoutPipe()
	defer func() {
		if err != nil {
			close(devices)
			close(parseErrors)
		}
	}()

	if err != nil {
		return err
	}

	rd := bufio.NewScanner(stdout)
	if err = cmd.Start(); err != nil {
		return err
	}

	go parseUdevadmOutput(cmd, rd, devices, parseErrors)

	return nil
}

func outputDevice(env map[string]string, devices chan<- *HotplugDeviceInfo, parseErrors chan<- error) {
	dev, err := NewHotplugDeviceInfo(env)
	if err != nil {
		parseErrors <- err
	} else {
		devices <- dev
	}
}
