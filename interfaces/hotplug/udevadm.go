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

const udevadmBin = `/sbin/udevadm`

var udevadmRe = regexp.MustCompile("^([A-Z]): (.*)$")

// RunUdevadm enumerates all devices by parsing 'udevadm info -e' command output and reports them
// via devices channel. Parsing errors are reported to parseErrors channel. Any fatal errors
// encountered when starting/stopping udevadm are reported by the error return value.
func RunUdevadm(devices chan<- *HotplugDeviceInfo, parseErrors chan<- error) error {
	cmd := exec.Command(udevadmBin, "info", "-e")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	rd := bufio.NewScanner(stdout)
	if err := cmd.Start(); err != nil {
		return err
	}

	env := make(map[string]string)
	for rd.Scan() {
		line := rd.Text()
		if line == "" {
			if len(env) > 0 {
				dev, err := NewHotplugDeviceInfo(env)
				if err != nil {
					parseErrors <- err
				} else {
					devices <- dev
				}
				env = make(map[string]string)
			}
		}
		m := udevadmRe.FindStringSubmatch(line)
		if len(m) > 0 {
			if m[1] == "E" {
				kv := strings.SplitN(m[2], "=", 2)
				if len(kv) == 2 {
					env[kv[0]] = env[kv[1]]
				} else {
					parseErrors <- fmt.Errorf("failed to parse udevadm output")
				}
			}
		}
	}

	if err := rd.Err(); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}
