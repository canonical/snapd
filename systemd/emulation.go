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
)

type emulation struct{}

var errNotImplemented = errors.New("not implemented in emulation mode")

func (s *emulation) DaemonReload() error {
	return errNotImplemented
}

func (s *emulation) Enable(service string) error {
	return errNotImplemented
}

func (s *emulation) Disable(service string) error {
	return errNotImplemented
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
	mountUnitName, options, err := writeMountUnitFile(snapName, revision, what, where, fstype)
	if err != nil {
		return "", err
	}

	cmd := exec.Command("mount", "-t", fstype, what, where, "-o", strings.Join(options, ","))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot mount %s (%s) at %s in pre-bake mode: %s; %s", what, where, fstype, err, string(out))
	}

	// cannot call systemd, so manually enable the unit by symlinking into multi-user.target.wants
	mu := MountUnitPath(where)
	enableUnitPath := filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants", mountUnitName)
	if err := osSymlink(mu, enableUnitPath); err != nil {
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
		if output, err := exec.Command("umount", "-d", "-l", mountedDir).CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	}

	enableUnitPathSymlink := filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants", filepath.Base(unit))
	if err := os.Remove(enableUnitPathSymlink); err != nil {
		return err
	}

	if err := os.Remove(unit); err != nil {
		return err
	}

	return nil
}

func (s *emulation) Mask(service string) error {
	return errNotImplemented
}

func (s *emulation) Unmask(service string) error {
	return errNotImplemented
}
