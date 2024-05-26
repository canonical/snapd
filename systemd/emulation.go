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

	"github.com/ddkwork/golibrary/mylog"
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
	_ := mylog.Check2(systemctlCmd(append([]string{"--root", s.rootDir, "enable"}, services...)...))
	return err
}

func (s *emulation) DisableNoReload(services []string) error {
	_ := mylog.Check2(systemctlCmd(append([]string{"--root", s.rootDir, "disable"}, services...)...))
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

func (s *emulation) ReloadOrRestart(services []string) error {
	return &notImplementedError{"ReloadOrRestart"}
}

func (s *emulation) RestartNoWaitForStop(services []string) error {
	return &notImplementedError{"RestartNoWaitForStop"}
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

func (s *emulation) EnsureMountUnitFile(description, what, where, fstype string, flags EnsureMountUnitFlags) (string, error) {
	// We don't build the options in exactly the same way as in the systemd
	// type because these options will be written in a unit that is used in
	// a host different to where this is running (the one used while
	// creating the preseeding tarball). Here we assume that the final
	// target is not a container.
	mountUnitOptions := append(fsMountOptions(fstype), squashfs.StandardOptions()...)
	return s.EnsureMountUnitFileWithOptions(&MountUnitOptions{
		Lifetime:                 Persistent,
		Description:              description,
		What:                     what,
		Where:                    where,
		Fstype:                   fstype,
		Options:                  mountUnitOptions,
		PreventRestartIfModified: flags.PreventRestartIfModified,
	})
}

func (s *emulation) EnsureMountUnitFileWithOptions(unitOptions *MountUnitOptions) (string, error) {
	if osutil.IsDirectory(unitOptions.What) {
		return "", fmt.Errorf("bind-mounted directory is not supported in emulation mode")
	}

	// Pass directly options, note that passed options need to be correct
	// for the final target that will use the preseeding tarball. See also
	// comment in EnsureMountUnitFile.
	mountUnitName, modified := mylog.Check3(ensureMountUnitFile(unitOptions))

	if modified == mountUnchanged {
		return mountUnitName, nil
	}
	mylog.Check(

		// Create directory as systemd would do when starting the unit
		os.MkdirAll(filepath.Join(dirs.GlobalRootDir, unitOptions.Where), 0755))

	// Here we need options that work for the system where we create the
	// tarball, so things are similar to what is done for
	// systemd.EnsureMountUnitFile. For instance, when preseeding in a lxd
	// container, the snap will be mounted with fuse, but mount unit will
	// use squashfs.
	hostFsType, actualOptions := hostFsTypeAndMountOptions(unitOptions.Fstype)
	if modified == mountUpdated {
		actualOptions = append(actualOptions, "remount")
	}
	cmd := exec.Command("mount", "-t", hostFsType, unitOptions.What, unitOptions.Where, "-o", strings.Join(actualOptions, ","))
	if out := mylog.Check2(cmd.CombinedOutput()); err != nil {
		return "", fmt.Errorf("cannot mount %s (%s) at %s in preseed mode: %s; %s", unitOptions.What, hostFsType, unitOptions.Where, err, string(out))
	}
	mylog.Check(s.EnableNoReload([]string{mountUnitName}))

	return mountUnitName, nil
}

func (s *emulation) RemoveMountUnitFile(mountedDir string) error {
	unit := MountUnitPath(dirs.StripRootDir(mountedDir))
	if !osutil.FileExists(unit) {
		return nil
	}

	isMounted := mylog.Check2(osutilIsMounted(mountedDir))

	if isMounted {
		// use detach-loop and lazy unmount
		if output := mylog.Check2(exec.Command("umount", "-d", "-l", mountedDir).CombinedOutput()); err != nil {
			return osutil.OutputErr(output, err)
		}
	}
	mylog.Check(s.DisableNoReload([]string{filepath.Base(unit)}))
	mylog.Check(os.Remove(unit))

	return nil
}

func (s *emulation) ListMountUnits(snapName, origin string) ([]string, error) {
	return nil, &notImplementedError{"ListMountUnits"}
}

func (s *emulation) Mask(service string) error {
	_ := mylog.Check2(systemctlCmd("--root", s.rootDir, "mask", service))
	return err
}

func (s *emulation) Unmask(service string) error {
	_ := mylog.Check2(systemctlCmd("--root", s.rootDir, "unmask", service))
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

func (s *emulation) SetLogLevel(logLevel string) error {
	return &notImplementedError{"SetLogLevel"}
}
