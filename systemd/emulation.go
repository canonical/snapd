// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package systemd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
)

type emulation struct {
	rootDir string
}

var errNotImplemented = errors.New("not implemented in emulation mode")

func (s *emulation) DaemonReload() error {
	return errNotImplemented
}

func (s *emulation) DaemonReexec() error {
	return errNotImplemented
}

func (s *emulation) Enable(service string) error {
	_, err := systemctlCmd("--root", s.rootDir, "enable", service)
	return err
}

func (s *emulation) Disable(service string) error {
	_, err := systemctlCmd("--root", s.rootDir, "disable", service)
	return err
}

func (s *emulation) Start(service ...string) error {
	return errNotImplemented
}

func (s *emulation) StartNoBlock(service ...string) error {
	return errNotImplemented
}

func (s *emulation) Stop(service string, timeout time.Duration) error {
	return errNotImplemented
}

func (s *emulation) Kill(service, signal, who string) error {
	return errNotImplemented
}

func (s *emulation) Restart(service string, timeout time.Duration) error {
	return errNotImplemented
}

func (s *emulation) ReloadOrRestart(service string) error {
	return errNotImplemented
}

func (s *emulation) RestartAll(service string) error {
	return errNotImplemented
}

func (s *emulation) Status(units ...string) ([]*UnitStatus, error) {
	return nil, errNotImplemented
}

func (s *emulation) IsEnabled(service string) (bool, error) {
	return false, errNotImplemented
}

func (s *emulation) IsActive(service string) (bool, error) {
	return false, errNotImplemented
}

func (s *emulation) LogReader(services []string, n int, follow bool) (io.ReadCloser, error) {
	return nil, errNotImplemented
}

func (s *emulation) AddMountUnitFile(snapName, revision, what, where, fstype string) (string, error) {
	if osutil.IsDirectory(what) {
		return "", fmt.Errorf("bind-mounted directory is not supported in emulation mode")
	}

	// In emulation mode hostFsType is the fs we want to use to manually mount
	// the snap below, but fstype is used for the created mount unit.
	// This means that when preseeding in a lxd container, the snap will be
	// mounted with fuse, but mount unit will use squashfs.
	mountUnitOptions := append(fsMountOptions(fstype), squashfs.StandardOptions()...)
	mountUnitName, err := writeMountUnitFile(snapName, revision, what, where, fstype, mountUnitOptions)
	if err != nil {
		return "", err
	}

	hostFsType, actualOptions := hostFsTypeAndMountOptions(fstype)
	cmd := exec.Command("mount", "-t", hostFsType, what, where, "-o", strings.Join(actualOptions, ","))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot mount %s (%s) at %s in preseed mode: %s; %s", what, hostFsType, where, err, string(out))
	}

	multiUserTargetWantsDir := filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants")
	if err := os.MkdirAll(multiUserTargetWantsDir, 0755); err != nil {
		return "", err
	}

	// cannot call systemd, so manually enable the unit by symlinking into multi-user.target.wants
	mu := MountUnitPath(where)
	enableUnitPath := filepath.Join(multiUserTargetWantsDir, mountUnitName)
	if err := os.Symlink(mu, enableUnitPath); err != nil {
		return "", fmt.Errorf("cannot enable mount unit %s: %v", mountUnitName, err)
	}
	return mountUnitName, nil
}

func (s *emulation) RemoveMountUnitFile(mountedDir string) error {
	unit := MountUnitPath(dirs.StripRootDir(mountedDir))
	if !osutil.FileExists(unit) {
		return nil
	}

	isMounted, err := osutilIsMounted(mountedDir)
	if err != nil {
		return err
	}
	if isMounted {
		// use detach-loop and lazy unmount
		if output, err := exec.Command("umount", "-d", "-l", mountedDir).CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	}

	multiUserTargetWantsDir := filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants")
	enableUnitPathSymlink := filepath.Join(multiUserTargetWantsDir, filepath.Base(unit))
	if err := os.Remove(enableUnitPathSymlink); err != nil {
		return err
	}

	if err := os.Remove(unit); err != nil {
		return err
	}

	return nil
}

func (s *emulation) Mask(service string) error {
	_, err := systemctlCmd("--root", s.rootDir, "mask", service)
	return err
}

func (s *emulation) Unmask(service string) error {
	_, err := systemctlCmd("--root", s.rootDir, "unmask", service)
	return err
}

func (s *emulation) Mount(what, where string, options ...string) error {
	return errNotImplemented
}

func (s *emulation) Umount(whatOrWhere string) error {
	return errNotImplemented
}
