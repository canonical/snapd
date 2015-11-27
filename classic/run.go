// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package classic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
)

type bindMount struct {
	src     string
	dst     string
	remount string
}

var bindMountDirs = []bindMount{
	{"/home", "/home", ""},
	{"/run", "/run", ""},
	{"/proc", "/proc", ""},
	{"/sys", "/sys", ""},
	{"/var/lib/extrausers", "/var/lib/extrausers", "ro"},
	{"/etc/sudoers", "/etc/sudoers", "ro"},
	{"/etc/sudoers.d", "/etc/sudoers.d", "ro"},
	{"/", "/snappy", ""},
}

// RunShell runs a shell in the classic environment
func RunShell() error {

	// setup the bind mounts
	for _, m := range bindMountDirs {
		if err := bindmount(m.src, m.dst, m.remount); err != nil {
			return err
		}
	}

	// drop to the calling user
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		sudoUser = "root"
	}

	// run chroot shell inside a systemd "scope"
	cmd := exec.Command("systemd-run", "--quiet", "--scope", "--unit=snappy-classic.scope", "--description=Snappy Classic shell", "chroot", dirs.ClassicDir, "sudo", "debian_chroot=classic", "-u", sudoUser, "-i")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// kill leftover processes after exiting, if it's still around
	cmd = exec.Command("systemctl", "stop", "snappy-classic.scope")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to cleanup classic: %s (%s)", err, output)
	}

	return nil
}

// Destroy destroys a classic environment, i.e. unmonts and removes all files
func Destroy() error {
	for _, m := range bindMountDirs {
		dst := filepath.Join(dirs.ClassicDir, m.dst)
		if isMounted(dst) {
			if err := umount(dst); err != nil {
				return err
			}
		}
	}

	return os.RemoveAll(dirs.ClassicDir)
}
