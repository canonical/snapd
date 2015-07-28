// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"os"
	"path/filepath"
	"strconv"
)

var commonSSHOptions = []string{"---", "ssh"}

// KvmSSHOptions returns a list with the adt-run ssh options to connect to the
// KVM testbed.
func KvmSSHOptions(imagePath string) []string {
	return append(
		commonSSHOptions,
		[]string{
			"-s", "/usr/share/autopkgtest/ssh-setup/snappy",
			"--", "-i", imagePath}...)
}

// RemoteTestbedSSHOptions returns a list with the adt-run ssh options to
// connect to a generic testbed.
func RemoteTestbedSSHOptions(testbedIP string, testbedPort int) []string {
	options := []string{
		"-H", testbedIP,
		"-p", strconv.Itoa(testbedPort),
		"-l", "ubuntu",
		"-i", filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
		"--reboot"}
	return append(commonSSHOptions, options...)
}
