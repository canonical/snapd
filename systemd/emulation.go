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
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
)

type emulation struct {
	rootDir string
}

type notImplementedError struct {
	op string
}

func (e *notImplementedError) Error() string {
	return fmt.Sprintf("%q is not implemented in emulation mode", e.op)
}

func (s *emulation) Backend() Backend {
	return EmulationModeBackend
}

func (s *emulation) DaemonReload() error {
	return nil
}

func (s *emulation) DaemonReexec() error {
	return &notImplementedError{"DaemonReexec"}
}

func (s *emulation) EnableNoReload(services []string) error {
	_, err := systemctlCmd(append([]string{"--root", s.rootDir, "enable"}, services...)...)
	return err
}

func (s *emulation) DisableNoReload(services []string) error {
	_, err := systemctlCmd(append([]string{"--root", s.rootDir, "disable"}, services...)...)
	return err
}

func (s *emulation) Start(services []string) error {
	return nil
}

func (s *emulation) StartNoBlock(services []string) error {
	return nil
}

func (s *emulation) Stop(services []string) error {
	return nil
}

func (s *emulation) Kill(service, signal, who string) error {
	return &notImplementedError{"Kill"}
}

func (s *emulation) Restart(services []string) error {
	return nil
}

func (s *emulation) ReloadOrRestart(service string) error {
	return &notImplementedError{"ReloadOrRestart"}
}

func (s *emulation) RestartAll(service string) error {
	return &notImplementedError{"RestartAll"}
}

func (s *emulation) Status(units []string) ([]*UnitStatus, error) {
	return nil, &notImplementedError{"Status"}
}

func (s *emulation) InactiveEnterTimestamp(unit string) (time.Time, error) {
	return time.Time{}, &notImplementedError{"InactiveEnterTimestamp"}
}

func (s *emulation) CurrentMemoryUsage(unit string) (quantity.Size, error) {
	return 0, &notImplementedError{"CurrentMemoryUsage"}
}

func (s *emulation) CurrentTasksCount(unit string) (uint64, error) {
	return 0, &notImplementedError{"CurrentTasksCount"}
}

func (s *emulation) IsEnabled(service string) (bool, error) {
	return false, &notImplementedError{"IsEnabled"}
}

func (s *emulation) IsActive(service string) (bool, error) {
	return false, &notImplementedError{"IsActive"}
}

func (s *emulation) LogReader(services []string, n int, follow, namespaces bool) (io.ReadCloser, error) {
	return nil, fmt.Errorf("LogReader")
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
	mountUnitName, err := writeMountUnitFile(&MountUnitOptions{
		Lifetime: Persistent,
		SnapName: snapName,
		Revision: revision,
		What:     what,
		Where:    where,
		Fstype:   fstype,
		Options:  mountUnitOptions,
	})
	if err != nil {
		return "", err
	}

	hostFsType, actualOptions := hostFsTypeAndMountOptions(fstype)
	cmd := exec.Command("mount", "-t", hostFsType, what, where, "-o", strings.Join(actualOptions, ","))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot mount %s (%s) at %s in preseed mode: %s; %s", what, hostFsType, where, err, string(out))
	}

	if err := s.EnableNoReload([]string{mountUnitName}); err != nil {
		return "", err
	}

	return mountUnitName, nil
}

func (s *emulation) AddMountUnitFileWithOptions(unitOptions *MountUnitOptions) (string, error) {
	return "", &notImplementedError{"AddMountUnitFileWithOptions"}
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

	if err := s.DisableNoReload([]string{filepath.Base(unit)}); err != nil {
		return err
	}

	if err := os.Remove(unit); err != nil {
		return err
	}

	return nil
}

func (s *emulation) ListMountUnits(snapName, origin string) ([]string, error) {
	return nil, &notImplementedError{"ListMountUnits"}
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
	return &notImplementedError{"Mount"}
}

func (s *emulation) Umount(whatOrWhere string) error {
	return &notImplementedError{"Umount"}
}

func (s *emulation) Run(command []string, opts *RunOptions) ([]byte, error) {
	return nil, &notImplementedError{"Run"}
}
