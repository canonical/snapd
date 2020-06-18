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

package disks

import "fmt"

func MockUdevPropertiesForDevice(new func(string) (map[string]string, error)) (restore func()) {
	old := udevadmProperties
	// for better testing we mock the udevadm command output so that we still
	// test the parsing
	udevadmProperties = func(dev string) ([]byte, error) {
		props, err := new(dev)
		if err != nil {
			return []byte(err.Error()), err
		}
		// put it into udevadm format output, i.e. "KEY=VALUE\n"
		output := ""
		for k, v := range props {
			output += fmt.Sprintf("%s=%s\n", k, v)
		}
		return []byte(output), nil
	}
	return func() {
		udevadmProperties = old
	}
}
