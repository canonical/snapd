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

package wrappers

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// catches units that run /usr/bin/snap (with args), or things in /usr/lib/snapd/
var execStartRe = regexp.MustCompile(`(?m)^ExecStart=(/usr/bin/snap\s+.*|/usr/lib/snapd/.*)$`)

// snapdToolingMountUnit is the name of the mount unit that provides the snapd tooling
const SnapdToolingMountUnit = "usr-lib-snapd.mount"

func snapdSkipStart(content []byte) bool {
	return bytes.Contains(content, []byte("X-Snapd-Snap: do-not-start"))
}

// snapdUnitSkipStart returns true for units that should not be started
// automatically
func snapdUnitSkipStart(unitPath string) (skip bool, err error) {
	content := mylog.Check2(os.ReadFile(unitPath))

	// no point in starting units that do not exist

	return snapdSkipStart(content), nil
}

func writeSnapdToolingMountUnit(sysd systemd.Systemd, prefix string, opts *AddSnapdSnapServicesOptions) error {
	// TODO: the following comment is wrong, we don't need RequiredBy=snapd here?

	// Not using EnsureMountUnitFile() because we need
	// "RequiredBy=snapd.service"

	content := []byte(fmt.Sprintf(`[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service
Before=systemd-udevd.service

[Mount]
What=%s/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
WantedBy=snapd.service
`, prefix))
	fullPath := filepath.Join(dirs.SnapServicesDir, SnapdToolingMountUnit)
	mylog.Check(osutil.EnsureFileState(fullPath,
		&osutil.MemoryFileState{
			Content: content,
			Mode:    0644,
		},
	))
	if err == osutil.ErrSameState {
		return nil
	}
	mylog.Check(sysd.DaemonReload())

	units := []string{SnapdToolingMountUnit}
	mylog.Check(sysd.EnableNoReload(units))
	mylog.Check(

		// meh this is killing snap services that use Requires=<this-unit> because
		// it doesn't use verbatim systemctl restart, it instead does it with
		// a systemctl stop and then a systemctl start, which triggers LP #1924805
		sysd.Restart(units))

	return nil
}

func undoSnapdToolingMountUnit(sysd systemd.Systemd) error {
	mountUnit := "usr-lib-snapd.mount"
	mountUnitPath := filepath.Join(dirs.SnapServicesDir, mountUnit)

	if !osutil.FileExists(mountUnitPath) {
		return nil
	}
	units := []string{mountUnit}
	mylog.Check(sysd.DisableNoReload(units))
	mylog.Check(

		// XXX: it is ok to stop the mount unit, the failover handler
		// executes snapd directly from the previous revision of snapd snap or
		// the core snap, the handler is running directly from the mounted snapd snap
		sysd.Stop(units))

	return os.Remove(mountUnitPath)
}

type AddSnapdSnapServicesOptions struct {
	// Preseeding is whether the system is currently being preseeded, in which
	// case there is not a running systemd for EnsureSnapServicesOptions to
	// issue commands like systemctl daemon-reload to.
	Preseeding bool
}

// SnapdRestart keeps state of services that need a restart after
// current symlink of snapd snap (/snap/snapd/current) has been
// updated. Call Restart method then.
// Currently, the list of services is static and the absence of a
// SnapdRestart object (nil), means no service requires a restart.
type SnapdRestart interface {
	Restart() error
}

type snapdRestartImpl struct {
	Sysd systemd.Systemd
}

// Restart restarts systemd service units. Call this method after
// symlink /snap/snapd/current has been updated.
func (r *snapdRestartImpl) Restart() error {
	mylog.Check(r.Sysd.StartNoBlock([]string{"snapd.apparmor.service"}))
	mylog.Check(

		// and finally start snapd.service (it will stop by itself and gets
		// started by systemd then)
		// Because of the file lock held on the snapstate by the Overlord, the new
		// snapd will block there until we release it. For this reason, we cannot
		// start the unit in blocking mode.
		// TODO: move/share this responsibility with daemon so that we can make the
		// start blocking again
		r.Sysd.StartNoBlock([]string{"snapd.service"}))
	mylog.Check(r.Sysd.StartNoBlock([]string{"snapd.seeded.service"}))
	mylog.Check(

		// we cannot start snapd.autoimport in blocking mode because
		// it has a "After=snapd.seeded.service" which means that on
		// seeding a "systemctl start" that blocks would hang forever
		// and we deadlock.
		r.Sysd.StartNoBlock([]string{"snapd.autoimport.service"}))

	return nil
}

// AddSnapdSnapServices sets up the services based on a given snapd snap in the
// system.
func AddSnapdSnapServices(s *snap.Info, opts *AddSnapdSnapServicesOptions, inter Interacter) (SnapdRestart, error) {
	if snapType := s.Type(); snapType != snap.TypeSnapd {
		return nil, fmt.Errorf("internal error: adding explicit snapd services for snap %q type %q is unexpected", s.InstanceName(), snapType)
	}

	// we never write snapd services on classic
	if release.OnClassic {
		return nil, nil
	}

	if opts == nil {
		opts = &AddSnapdSnapServicesOptions{}
	}

	var sysd systemd.Systemd
	if !opts.Preseeding {
		sysd = systemd.New(systemd.SystemMode, inter)
	} else {
		sysd = systemd.NewEmulationMode("")
	}
	mylog.Check(writeSnapdToolingMountUnit(sysd, s.MountDir(), opts))

	serviceUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.service")))

	socketUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.socket")))

	timerUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.timer")))

	targetUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.target")))

	units := append(socketUnits, serviceUnits...)
	units = append(units, timerUnits...)
	units = append(units, targetUnits...)

	snapdUnits := make(map[string]osutil.FileState, len(units)+1)
	for _, unit := range units {
		st := mylog.Check2(os.Stat(unit))

		content := mylog.Check2(os.ReadFile(unit))

		if execStartRe.Match(content) {
			content = execStartRe.ReplaceAll(content, []byte(fmt.Sprintf("ExecStart=%s$1", s.MountDir())))
			// when the service executes a command from the snapd snap, make
			// sure the exec path points to the mount dir, and that the
			// mount happens before the unit is started
			content = append(content, []byte(fmt.Sprintf("\n[Unit]\nRequiresMountsFor=%s\n", s.MountDir()))...)
		}

		snapdUnits[filepath.Base(unit)] = &osutil.MemoryFileState{
			Content: content,
			Mode:    st.Mode(),
		}
	}
	globs := []string{"snapd.service", "snapd.socket", "snapd.*.service", "snapd.*.timer", "snapd.*.target"}
	changed, removed := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapServicesDir, globs, snapdUnits))

	// TODO: uhhhh, what do we do in this case?

	if (len(changed) + len(removed)) == 0 {
		// nothing to do
		return nil, nil
	}

	// stop all removed units first
	for _, unit := range removed {
		serviceUnits := []string{unit}
		mylog.Check(sysd.Stop(serviceUnits))
		mylog.Check(sysd.DisableNoReload(serviceUnits))

	}

	// daemon-reload so that we get the new services
	if len(changed) > 0 {
		mylog.Check(sysd.DaemonReload())
	}

	// enable/start all the new services
	for _, unit := range changed {
		// systemd looks at the logical units, even if 'enabled' service
		// symlink points to /lib/systemd/system location, dropping an
		// identically named service in /etc overrides the other unit,
		// therefore it is sufficient to enable the new units only
		//
		// Calling sysd.Enable() unconditionally may fail depending on
		// systemd version, where older versions (eg 229 in 16.04) would
		// error out unless --force is passed, while new ones remove the
		// symlink and create a new one.
		if !opts.Preseeding {
			enabled := mylog.Check2(sysd.IsEnabled(unit))

			if enabled {
				continue
			}
		}
		mylog.Check(sysd.EnableNoReload([]string{unit}))

	}

	if !opts.Preseeding {
		for _, unit := range changed {
			// Some units (like the snapd.system-shutdown.service) cannot
			// be started. Others like "snapd.seeded.service" are started
			// as dependencies of snapd.service.
			if snapdSkipStart(snapdUnits[unit].(*osutil.MemoryFileState).Content) {
				continue
			}
			// Ensure to only restart if the unit was previously
			// active. This ensures we DTRT on firstboot and do
			// not stop e.g. snapd.socket because doing that
			// would mean that the snapd.seeded.service is also
			// stopped (independently of snapd.socket being
			// active) which confuses the boot order (the unit
			// exists before we are fully seeded).
			isActive := mylog.Check2(sysd.IsActive(unit))

			serviceUnits := []string{unit}
			if isActive {
				// we can never restart the snapd.socket because
				// this will also bring down snapd itself
				if unit != "snapd.socket" {
					mylog.Check(sysd.Restart(serviceUnits))
				}
			} else {
				mylog.Check(sysd.Start(serviceUnits))
			}
		}
	}
	mylog.Check(

		// Handle the user services
		writeSnapdUserServicesOnCore(s, opts, inter))
	mylog.Check(

		// Handle D-Bus configuration
		writeSnapdDbusConfigOnCore(s))
	mylog.Check(writeSnapdDbusActivationOnCore(s))
	mylog.Check(writeSnapdDesktopFilesOnCore(s))

	return &snapdRestartImpl{Sysd: sysd}, nil
}

// undoSnapdUserServicesOnCore attempts to remove services that were deployed in
// the filesystem as part of snapd snap installation. This should only be
// executed as part of a controlled undo path.
func undoSnapdServicesOnCore(s *snap.Info, sysd systemd.Systemd) error {
	// list service, socket and timer units present in the snapd snap
	serviceUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.service")))

	socketUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.socket")))

	timerUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.timer")))

	targetUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.target")))

	units := append(socketUnits, serviceUnits...)
	units = append(units, timerUnits...)
	units = append(units, targetUnits...)

	for _, snapdUnit := range units {
		sysdUnit := filepath.Base(snapdUnit)
		coreUnit := filepath.Join(dirs.GlobalRootDir, "lib/systemd/system", sysdUnit)
		writtenUnitPath := filepath.Join(dirs.SnapServicesDir, sysdUnit)
		if !osutil.FileExists(writtenUnitPath) {
			continue
		}
		existsInCore := osutil.FileExists(coreUnit)

		unit := []string{sysdUnit}
		if !existsInCore {
			mylog.Check(
				// new unit that did not exist on core, disable and stop
				sysd.DisableNoReload(unit))
			mylog.Check(sysd.Stop(unit))

		}
		mylog.Check(os.Remove(writtenUnitPath))

		if !existsInCore {
			// nothing more to do here
			continue
		}

		isEnabled := mylog.Check2(sysd.IsEnabled(sysdUnit))

		if !isEnabled {
			mylog.Check(sysd.EnableNoReload(unit))
		}

		if sysdUnit == "snapd.socket" {
			// do not start the socket, snap failover handler will
			// restart it
			continue
		}
		skipStart := mylog.Check2(snapdUnitSkipStart(coreUnit))

		if !skipStart {
			// TODO: consider using sys.Restart() instead of is-active check
			isActive := mylog.Check2(sysd.IsActive(sysdUnit))

			if isActive {
				mylog.Check(sysd.Restart(unit))
			} else {
				mylog.Check(sysd.Start(unit))
			}
		}
	}
	return nil
}

func writeSnapdUserServicesOnCore(s *snap.Info, opts *AddSnapdSnapServicesOptions, inter Interacter) error {
	mylog.Check(
		// Ensure /etc/systemd/user exists
		os.MkdirAll(dirs.SnapUserServicesDir, 0755))

	// TODO: use EmulationMode when preseeding (teach EmulationMode about user services)?
	sysd := systemd.New(systemd.GlobalUserMode, inter)

	serviceUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.service")))

	socketUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.socket")))

	units := append(serviceUnits, socketUnits...)

	snapdUnits := make(map[string]osutil.FileState, len(units)+1)
	for _, unit := range units {
		st := mylog.Check2(os.Stat(unit))

		content := mylog.Check2(os.ReadFile(unit))

		if execStartRe.Match(content) {
			content = execStartRe.ReplaceAll(content, []byte(fmt.Sprintf("ExecStart=%s$1", s.MountDir())))
			// when the service executes a command from the snapd snap, make
			// sure the exec path points to the mount dir, and that the
			// mount happens before the unit is started
			content = append(content, []byte(fmt.Sprintf("\n[Unit]\nRequiresMountsFor=%s\n", s.MountDir()))...)
		}

		snapdUnits[filepath.Base(unit)] = &osutil.MemoryFileState{
			Content: content,
			Mode:    st.Mode(),
		}
	}
	changed, removed := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapUserServicesDir, []string{"snapd.*.service", "snapd.*.socket"}, snapdUnits))

	// TODO: uhhhh, what do we do in this case?

	if (len(changed) + len(removed)) == 0 {
		// nothing to do
		return nil
	}
	// disable all removed units first
	for _, unit := range removed {
		mylog.Check(sysd.DisableNoReload([]string{unit}))
	}

	// enable/start all the new services
	for _, unit := range changed {
		units := []string{unit}
		mylog.Check(sysd.DisableNoReload(units))
		mylog.Check(sysd.EnableNoReload(units))

	}

	if !opts.Preseeding {
		mylog.Check(userDaemonReload())
	}

	return nil
}

// undoSnapdUserServicesOnCore attempts to remove user services that were
// deployed in the filesystem as part of snapd snap installation. This should
// only be executed as part of a controlled undo path.
func undoSnapdUserServicesOnCore(s *snap.Info, inter Interacter) error {
	sysd := systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.GlobalUserMode, inter)

	// list user service and socket units present in the snapd snap
	serviceUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.service")))

	socketUnits := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.socket")))

	units := append(serviceUnits, socketUnits...)

	for _, srcUnit := range units {
		unit := filepath.Base(srcUnit)
		writtenUnitPath := filepath.Join(dirs.SnapUserServicesDir, unit)
		if !osutil.FileExists(writtenUnitPath) {
			continue
		}
		coreUnit := filepath.Join(dirs.GlobalRootDir, "usr/lib/systemd/user", unit)
		existsInCore := osutil.FileExists(coreUnit)
		mylog.Check(sysd.DisableNoReload([]string{unit}))
		mylog.Check(os.Remove(writtenUnitPath))

		if !existsInCore {
			// new unit that did not exist on core
			continue
		}
		mylog.Check(sysd.EnableNoReload([]string{unit}))

	}
	return nil
}

func DeriveSnapdDBusConfig(s *snap.Info) (sessionContent, systemContent map[string]osutil.FileState, err error) {
	sessionConfigs := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "usr/share/dbus-1/session.d/snapd.*.conf")))

	sessionContent = make(map[string]osutil.FileState, len(sessionConfigs)+1)
	for _, config := range sessionConfigs {
		sessionContent[filepath.Base(config)] = &osutil.FileReference{
			Path: config,
		}
	}

	systemConfigs := mylog.Check2(filepath.Glob(filepath.Join(s.MountDir(), "usr/share/dbus-1/system.d/snapd.*.conf")))

	systemContent = make(map[string]osutil.FileState, len(systemConfigs)+1)
	for _, config := range systemConfigs {
		systemContent[filepath.Base(config)] = &osutil.FileReference{
			Path: config,
		}
	}

	return sessionContent, systemContent, nil
}

func isReadOnlyFsError(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*os.PathError); ok {
		err = e.Err
	}
	if e, ok := err.(syscall.Errno); ok {
		return e == syscall.EROFS
	}
	return false
}

var ensureDirState = osutil.EnsureDirState

func writeSnapdDbusConfigOnCore(s *snap.Info) error {
	sessionContent, systemContent := mylog.Check3(DeriveSnapdDBusConfig(s))

	_, _ = mylog.Check3(ensureDirState(dirs.SnapDBusSessionPolicyDir, "snapd.*.conf", sessionContent))

	// If /etc/dbus-1/session.d is read-only (which may be the case on very old core18), then
	// err is os.PathError with syscall.Errno underneath. Hitting this prevents snapd refresh,
	// so log the error but carry on. This fixes LP: 1899664.
	// XXX: ideally we should regenerate session files elsewhere if we fail here (otherwise
	// this will only happen on future snapd refresh), but realistically this
	// is not relevant on core18 devices.

	_, _ = mylog.Check3(osutil.EnsureDirState(dirs.SnapDBusSystemPolicyDir, "snapd.*.conf", systemContent))

	return nil
}

func undoSnapdDbusConfigOnCore() error {
	_, _ := mylog.Check3(osutil.EnsureDirState(dirs.SnapDBusSystemPolicyDir, "snapd.*.conf", nil))

	_, _ = mylog.Check3(osutil.EnsureDirState(dirs.SnapDBusSessionPolicyDir, "snapd.*.conf", nil))
	return err
}

// Service files that have been written by snapd.
// Only ever append this list, if a file is no longer installed it needs to
// remain here so a newer version of snapd can remove it.
var dbusSessionServices = []string{
	"io.snapcraft.Launcher.service",
	"io.snapcraft.Prompt.service",
	"io.snapcraft.Settings.service",
	"io.snapcraft.SessionAgent.service",
}

func writeSnapdDbusActivationOnCore(s *snap.Info) error {
	mylog.Check(os.MkdirAll(dirs.SnapDBusSessionServicesDir, 0755))

	content := make(map[string]osutil.FileState, len(dbusSessionServices)+1)
	for _, service := range dbusSessionServices {
		filePathInSnap := filepath.Join(s.MountDir(), "usr/share/dbus-1/services", service)
		if !osutil.FileExists(filePathInSnap) {
			continue
		}
		content[service] = &osutil.FileReference{
			Path: filePathInSnap,
		}
	}

	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapDBusSessionServicesDir, dbusSessionServices, content))
	return err
}

func undoSnapdDbusActivationOnCore() error {
	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapDBusSessionServicesDir, dbusSessionServices, nil))
	return err
}

// Desktop files that have been written by snapd.
// Only ever append this list, if a file is no longer installed it needs to
// remain here so a newer version of snapd can remove it.
var snapdDesktopFileNames = []string{
	"io.snapcraft.SessionAgent.desktop",
	"snap-handle-link.desktop",
}

func writeSnapdDesktopFilesOnCore(s *snap.Info) error {
	mylog.Check(
		// Ensure /var/lib/snapd/desktop/applications exists
		os.MkdirAll(dirs.SnapDesktopFilesDir, 0755))

	desktopFiles := make(map[string]osutil.FileState, len(snapdDesktopFileNames))
	for _, fileName := range snapdDesktopFileNames {
		filePathInSnap := filepath.Join(s.MountDir(), "usr/share/applications", fileName)
		if !osutil.FileExists(filePathInSnap) {
			continue
		}
		desktopFiles[fileName] = &osutil.FileReference{Path: filePathInSnap}
	}

	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapDesktopFilesDir, snapdDesktopFileNames, desktopFiles))
	return err
}

func undoSnapdDesktopFilesOnCore(s *snap.Info) error {
	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapDesktopFilesDir, snapdDesktopFileNames, nil))
	return err
}

// RemoveSnapdSnapServicesOnCore removes the snapd services generated by a prior
// call to AddSnapdSnapServices. The core snap is used as the reference for
// restoring the system state, making this undo helper suitable for use when
// reverting the first installation of the snapd snap on a core device.
func RemoveSnapdSnapServicesOnCore(s *snap.Info, inter Interacter) error {
	if snapType := s.Type(); snapType != snap.TypeSnapd {
		return fmt.Errorf("internal error: removing explicit snapd services for snap %q type %q is unexpected", s.InstanceName(), snapType)
	}

	// snapd services are never written on classic, nothing to remove
	if release.OnClassic {
		return nil
	}

	sysd := systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.SystemMode, inter)
	mylog.Check(undoSnapdDesktopFilesOnCore(s))
	mylog.Check(undoSnapdDbusActivationOnCore())
	mylog.Check(undoSnapdDbusConfigOnCore())
	mylog.Check(undoSnapdServicesOnCore(s, sysd))
	mylog.Check(undoSnapdUserServicesOnCore(s, inter))
	mylog.Check(undoSnapdToolingMountUnit(sysd))

	// XXX: reload after all operations?
	return nil
}
