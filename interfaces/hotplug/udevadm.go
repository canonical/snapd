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
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

var udevadmBin = `udevadm`

func parseEnvBlock(block string) (map[string]string, error) {
	env := make(map[string]string)
	for i, line := range strings.Split(block, "\n") {
		if i == 0 && !strings.HasPrefix(line, "P: ") {
			return nil, fmt.Errorf("no device block marker found before %q", line)
		}
		// We are only interested in 'E' properties as they carry all the interesting data,
		// including DEVPATH and DEVNAME which seem to mirror the 'P' and 'N' values.
		if strings.HasPrefix(line, "E: ") {
			if kv := strings.SplitN(line[3:], "=", 2); len(kv) == 2 {
				env[kv[0]] = kv[1]
			} else {
				return nil, fmt.Errorf("failed to parse udevadm output %q", line)
			}
		}
	}
	return env, nil
}

func scanDoubleNewline(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		// we found data
		return i + 2, data[0:i], nil
	}

	// If we're at EOF, return what is left.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}

func parseUdevadmOutput(cmd *exec.Cmd, rd *bufio.Scanner, devices chan<- *HotplugDeviceInfo, parseErrors chan<- error) {
	defer close(devices)
	defer close(parseErrors)

	env := make(map[string]string)

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
		block := rd.Text()
		env, err := parseEnvBlock(block)
		if err != nil {
			parseErrors <- err
		} else {
			outputDevice(env, devices, parseErrors)
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
	rd.Split(scanDoubleNewline)
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
