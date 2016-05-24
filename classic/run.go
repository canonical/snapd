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
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/strutil"
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
	{"/dev", "/dev", ""},
	{"/var/lib/extrausers", "/var/lib/extrausers", "ro"},
	{"/etc/sudoers", "/etc/sudoers", "ro"},
	{"/", "/snappy", ""},
}

// genClassicScopeName generates an uniq name for the classic scope
func genClassicScopeName() string {
	now := time.Now()
	ti := fmt.Sprintf("%4d-%02d-%02d_%02d:%02d:%02d", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second())
	return fmt.Sprintf("snappy-classic_%s_%s.scope", ti, strutil.MakeRandomString(5))
}

func runInClassicEnv(cmdStr ...string) error {
	// setup the bind mounts
	for _, m := range bindMountDirs {
		if err := bindmount(m.src, m.dst, m.remount); err != nil {
			return err
		}
	}

	// run chroot shell inside a systemd "scope"
	unitName := genClassicScopeName()
	args := []string{"systemd-run", "--quiet", "--scope", fmt.Sprintf("--unit=%s", unitName), "--description=Snappy Classic shell", "chroot", dirs.ClassicDir}
	args = append(args, cmdStr...)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunShell runs a shell in the classic environment
func RunShell() error {
	// drop to the calling user
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		sudoUser = "root"
	}
	runInClassicEnv("sudo", "debian_chroot=classic", "-u", sudoUser, "-i")

	// We could inform the user here that
	//  "systemctl stop $unitName"
	// will kill all leftover processes inside the classic scope.
	//
	// But its also easy to do manually if needed via:
	//  "systemctl status |grep snappy-classic"
	//  "systemctl stop $unitName
	// so we do not clutter the output here.

	return nil
}

func unmountBindMounts() error {
	for _, m := range bindMountDirs {
		dst := filepath.Join(dirs.ClassicDir, m.dst)
		needsUnmount, err := isMounted(dst)
		if err != nil {
			return err
		}
		if needsUnmount {
			if err := umount(dst); err != nil {
				return err
			}
		}
	}

	return nil
}

// Destroy destroys a classic environment, i.e. unmonts and removes all files
func Destroy() error {
	if err := unmountBindMounts(); err != nil {
		return fmt.Errorf("cannot unmount")
	}

	return os.RemoveAll(dirs.ClassicDir)
}
