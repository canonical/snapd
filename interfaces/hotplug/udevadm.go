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
	"regexp"
	"strings"
)

var udevadmBin = `/sbin/udevadm`
var udevadmRe = regexp.MustCompile("^([A-Z]): (.*)$")

// RunUdevadm enumerates all devices by parsing 'udevadm info -e' command output and reports them
// via devices channel. The devices channel gets closed to indicate that all devices were processed.
// Parsing errors are reported to parseErrors channel. Fatal errors encountered when starting udevadm
// are reported by the error return value.
func RunUdevadm(devices chan<- *HotplugDeviceInfo, parseErrors chan<- error) error {
	cmd := exec.Command(udevadmBin, "info", "-e")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		close(devices)
		return err
	}

	rd := bufio.NewScanner(stdout)
	if err := cmd.Start(); err != nil {
		close(devices)
		return err
	}

	go func() {
		defer close(devices)

		env := make(map[string]string)
		for rd.Scan() {
			line := rd.Text()
			if line == "" {
				if len(env) > 0 {
					outputDevice(env, devices, parseErrors)
					env = make(map[string]string)
				}
				continue
			}
			match := udevadmRe.FindStringSubmatch(line)
			if len(match) > 0 {
				// possible udevadm line prefixes are:
				//  'P' - device path (e.g /devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0)
				//  'N' - device node (e.g. "/dev/ttyUSB0")
				//  'L' - devlink priority (unclear)
				//  'S' - devlinks (e.g. serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0)
				//  'E' - property (key=value format)
				// We are only interested in 'E' properties as they carry all the interesting data,
				// including DEVPATH and DEVNAME which seem to mirror the 'P' and 'N' values.
				if match[1] == "E" {
					kv := strings.SplitN(match[2], "=", 2)
					if len(kv) == 2 {
						env[kv[0]] = kv[1]
					} else {
						parseErrors <- fmt.Errorf("failed to parse udevadm output %q", line)
					}
				}
			} else {
				parseErrors <- fmt.Errorf("failed to parse udevadm output %q", line)
			}
		}

		// eof, flush remaing device
		if len(env) > 0 {
			outputDevice(env, devices, parseErrors)
		}

		if err := rd.Err(); err != nil {
			parseErrors <- fmt.Errorf("udevadm command failed: %s", err)
		}
		if err := cmd.Wait(); err != nil {
			parseErrors <- fmt.Errorf("udevadm command failed: %s", err)
		}
	}()

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
