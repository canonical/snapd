// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package autopkgtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	commonSSHOptions = "--- ssh "
	sshTimeout       = 600
)

func kvmSSHOptions(imagePath string) string {
	return fmt.Sprint(commonSSHOptions,
		"-s /usr/share/autopkgtest/ssh-setup/snappy -- -b -i ", imagePath)
}

func remoteTestbedSSHOptions(testbedIP string, testbedPort int) string {
	return fmt.Sprint(commonSSHOptions,
		"-H ", testbedIP,
		" -p ", strconv.Itoa(testbedPort),
		" -l ubuntu",
		" -i ", filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
		" --reboot",
		" --timeout-ssh ", strconv.Itoa(sshTimeout))
}
