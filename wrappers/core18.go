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
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
)

var snapdServiceStopTimeout = time.Duration(timeout.DefaultTimeout)

// catches units that run /usr/bin/snap (with args), or things in /usr/lib/snapd/
var execStartRe = regexp.MustCompile(`(?m)^ExecStart=(/usr/bin/snap\s+.*|/usr/lib/snapd/.*)$`)

func snapdSkipStart(content []byte) bool {
	return bytes.Contains(content, []byte("X-Snapd-Snap: do-not-start"))
}

// snapdUnitSkipStart returns true for units that should not be started
// automatically
func snapdUnitSkipStart(unitPath string) (skip bool, err error) {
	content, err := ioutil.ReadFile(unitPath)
	if err != nil {
		if os.IsNotExist(err) {
			// no point in starting units that do not exist
			return true, nil
		}
		return false, err
	}
	return snapdSkipStart(content), nil
}

func writeSnapdToolingMountUnit(sysd systemd.Systemd, prefix string) error {
	// Not using AddMountUnitFile() because we need
	// "RequiredBy=snapd.service"
	content := []byte(fmt.Sprintf(`[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service

[Mount]
What=%s/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
WantedBy=snapd.service
`, prefix))
	unit := "usr-lib-snapd.mount"
	fullPath := filepath.Join(dirs.SnapServicesDir, unit)

	err := osutil.EnsureFileState(fullPath,
		&osutil.MemoryFileState{
			Content: content,
			Mode:    0644,
		},
	)
	if err == osutil.ErrSameState {
		return nil
	}
	if err != nil {
		return err
	}
	if err := sysd.DaemonReload(); err != nil {
		return err
	}
	if err := sysd.Enable(unit); err != nil {
		return err
	}

	if err := sysd.Restart(unit, 5*time.Second); err != nil {
		return err
	}

	return nil
}

func undoSnapdToolingMountUnit(sysd systemd.Systemd) error {
	unit := "usr-lib-snapd.mount"
	mountUnitPath := filepath.Join(dirs.SnapServicesDir, unit)

	if !osutil.FileExists(mountUnitPath) {
		return nil
	}

	if err := sysd.Disable(unit); err != nil {
		return err
	}
	// XXX: it is ok to stop the mount unit, the failover handler
	// executes snapd directly from the previous revision of snapd snap or
	// the core snap, the handler is running directly from the mounted snapd snap
	if err := sysd.Stop(unit, snapdServiceStopTimeout); err != nil {
		return err
	}
	return os.Remove(mountUnitPath)
}

// AddSnapdSnapServices sets up the services based on a given snapd snap in the
// system.
func AddSnapdSnapServices(s *snap.Info, inter interacter) error {
	// we never write
	if release.OnClassic {
		return nil
	}
	if snapType := s.GetType(); snapType != snap.TypeSnapd {
		return fmt.Errorf("internal error: cannot add snapd services of snap %q type %q", s.InstanceName(), snapType)
	}

	sysd := systemd.New(dirs.GlobalRootDir, systemd.SystemMode, inter)

	if err := writeSnapdToolingMountUnit(sysd, s.MountDir()); err != nil {
		return err
	}

	serviceUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.service"))
	if err != nil {
		return err
	}
	socketUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.socket"))
	if err != nil {
		return err
	}
	timerUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.timer"))
	if err != nil {
		return err
	}
	units := append(socketUnits, serviceUnits...)
	units = append(units, timerUnits...)

	snapdUnits := make(map[string]osutil.FileState, len(units)+1)
	for _, unit := range units {
		st, err := os.Stat(unit)
		if err != nil {
			return err
		}
		content, err := ioutil.ReadFile(unit)
		if err != nil {
			return err
		}
		content = execStartRe.ReplaceAll(content, []byte(fmt.Sprintf(`ExecStart=%s$1`, s.MountDir())))

		snapdUnits[filepath.Base(unit)] = &osutil.MemoryFileState{
			Content: content,
			Mode:    st.Mode(),
		}
	}
	globs := []string{"snapd.service", "snapd.socket", "snapd.*.service", "snapd.*.timer"}
	changed, removed, err := osutil.EnsureDirStateGlobs(dirs.SnapServicesDir, globs, snapdUnits)
	if err != nil {
		// TODO: uhhhh, what do we do in this case?
		return err
	}
	if (len(changed) + len(removed)) == 0 {
		// nothing to do
		return nil
	}
	// stop all removed units first
	for _, unit := range removed {
		if err := sysd.Stop(unit, 5*time.Second); err != nil {
			logger.Noticef("failed to stop %q: %v", unit, err)
		}
		if err := sysd.Disable(unit); err != nil {
			logger.Noticef("failed to disable %q: %v", unit, err)
		}
	}

	// daemon-reload so that we get the new services
	if len(changed) > 0 {
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
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
		enabled, err := sysd.IsEnabled(unit)
		if err != nil {
			return err
		}
		if enabled {
			continue
		}
		if err := sysd.Enable(unit); err != nil {
			return err
		}
	}

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
		isActive, err := sysd.IsActive(unit)
		if err != nil {
			return err
		}
		if isActive {
			// we can never restart the snapd.socket because
			// this will also bring down snapd itself
			if unit != "snapd.socket" {
				if err := sysd.Restart(unit, 5*time.Second); err != nil {
					return err
				}
			}
		} else {
			if err := sysd.Start(unit); err != nil {
				return err
			}
		}
	}
	// and finally start snapd.service (it will stop by itself and gets
	// started by systemd then)
	if err := sysd.Start("snapd.service"); err != nil {
		return err
	}
	if err := sysd.StartNoBlock("snapd.seeded.service"); err != nil {
		return err
	}
	// we cannot start snapd.autoimport in blocking mode because
	// it has a "After=snapd.seeded.service" which means that on
	// seeding a "systemctl start" that blocks would hang forever
	// and we deadlock.
	if err := sysd.StartNoBlock("snapd.autoimport.service"); err != nil {
		return err
	}

	// Handle the user services
	if err := writeSnapdUserServicesOnCore(s, inter); err != nil {
		return err
	}

	return nil
}

// undoSnapdUserServicesOnCore attempts to remove services that were deployed in
// the filesystem as part of snapd snap installation. This should only be
// executed as part of a controlled undo path.
func undoSnapdServicesOnCore(s *snap.Info, sysd systemd.Systemd) error {
	// list service, socket and timer units present in the snapd snap
	serviceUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.service"))
	if err != nil {
		return err
	}
	socketUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.socket"))
	if err != nil {
		return err
	}
	timerUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.timer"))
	if err != nil {
		return err
	}
	units := append(socketUnits, serviceUnits...)
	units = append(units, timerUnits...)

	for _, snapdUnit := range units {
		unit := filepath.Base(snapdUnit)
		coreUnit := filepath.Join(dirs.GlobalRootDir, "lib/systemd/system", unit)
		writtenUnitPath := filepath.Join(dirs.SnapServicesDir, unit)
		if !osutil.FileExists(writtenUnitPath) {
			continue
		}
		existsInCore := osutil.FileExists(coreUnit)

		if !existsInCore {
			// new unit that did not exist on core, disable and stop
			if err := sysd.Disable(unit); err != nil {
				logger.Noticef("failed to disable %q: %v", unit, err)
			}
			if err := sysd.Stop(unit, snapdServiceStopTimeout); err != nil {
				return err
			}
		}
		if err := os.Remove(writtenUnitPath); err != nil {
			return err
		}
		if !existsInCore {
			// nothing more to do here
			continue
		}

		isEnabled, err := sysd.IsEnabled(unit)
		if err != nil {
			return err
		}
		if !isEnabled {
			if err := sysd.Enable(unit); err != nil {
				return err
			}
		}

		if unit == "snapd.socket" {
			// do not start the socket, snap failover handler will
			// restart it
			continue
		}
		skipStart, err := snapdUnitSkipStart(coreUnit)
		if err != nil {
			return err
		}
		if !skipStart {
			// TODO: consider using sys.Restart() instead of is-active check
			isActive, err := sysd.IsActive(unit)
			if err != nil {
				return err
			}
			if isActive {
				if err := sysd.Restart(unit, snapdServiceStopTimeout); err != nil {
					return err
				}
			} else {
				if err := sysd.Start(unit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func writeSnapdUserServicesOnCore(s *snap.Info, inter interacter) error {
	// Ensure /etc/systemd/user exists
	if err := os.MkdirAll(dirs.SnapUserServicesDir, 0755); err != nil {
		return err
	}

	sysd := systemd.New(dirs.GlobalRootDir, systemd.GlobalUserMode, inter)

	serviceUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.service"))
	if err != nil {
		return err
	}
	socketUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.socket"))
	if err != nil {
		return err
	}
	units := append(serviceUnits, socketUnits...)

	snapdUnits := make(map[string]osutil.FileState, len(units)+1)
	for _, unit := range units {
		st, err := os.Stat(unit)
		if err != nil {
			return err
		}
		content, err := ioutil.ReadFile(unit)
		if err != nil {
			return err
		}
		content = execStartRe.ReplaceAll(content, []byte(fmt.Sprintf(`ExecStart=%s$1`, s.MountDir())))

		snapdUnits[filepath.Base(unit)] = &osutil.MemoryFileState{
			Content: content,
			Mode:    st.Mode(),
		}
	}
	changed, removed, err := osutil.EnsureDirStateGlobs(dirs.SnapUserServicesDir, []string{"snapd.*.service", "snapd.*.socket"}, snapdUnits)
	if err != nil {
		// TODO: uhhhh, what do we do in this case?
		return err
	}
	if (len(changed) + len(removed)) == 0 {
		// nothing to do
		return nil
	}
	// disable all removed units first
	for _, unit := range removed {
		if err := sysd.Disable(unit); err != nil {
			logger.Noticef("failed to disable %q: %v", unit, err)
		}
	}

	// enable/start all the new services
	for _, unit := range changed {
		if err := sysd.Disable(unit); err != nil {
			logger.Noticef("failed to disable %q: %v", unit, err)
		}
		if err := sysd.Enable(unit); err != nil {
			return err
		}
	}

	return nil
}

// undoSnapdUserServicesOnCore attempts to remove user services that were
// deployed in the filesystem as part of snapd snap installation. This should
// only be executed as part of a controlled undo path.
func undoSnapdUserServicesOnCore(s *snap.Info, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, systemd.GlobalUserMode, inter)

	// list user service and socket units present in the snapd snap
	serviceUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.service"))
	if err != nil {
		return err
	}
	socketUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "usr/lib/systemd/user/*.socket"))
	if err != nil {
		return err
	}
	units := append(serviceUnits, socketUnits...)

	for _, srcUnit := range units {
		unit := filepath.Base(srcUnit)
		writtenUnitPath := filepath.Join(dirs.SnapUserServicesDir, unit)
		if !osutil.FileExists(writtenUnitPath) {
			continue
		}
		coreUnit := filepath.Join(dirs.GlobalRootDir, "usr/lib/systemd/user", unit)
		existsInCore := osutil.FileExists(coreUnit)

		if err := sysd.Disable(unit); err != nil {
			logger.Noticef("failed to disable %q: %v", unit, err)
		}
		if err := os.Remove(writtenUnitPath); err != nil {
			return err
		}
		if !existsInCore {
			// new unit that did not exist on core
			continue
		}
		if err := sysd.Enable(unit); err != nil {
			return err
		}
	}
	return nil
}

// RemoveSnapdSnapServicesOnCore removes the snapd services generated by a prior
// call to AddSnapdSnapServices. The core snap is used as the reference for
// restoring the system state, making this undo helper suitable for use when
// reverting the first installation of the snapd snap on a core device.
func RemoveSnapdSnapServicesOnCore(s *snap.Info, inter interacter) error {
	// nothing to do on classic
	if release.OnClassic {
		return nil
	}

	if snapType := s.GetType(); snapType != snap.TypeSnapd {
		return fmt.Errorf("internal error: cannot remove snapd services of snap %q type %q", s.InstanceName(), snapType)
	}

	sysd := systemd.New(dirs.GlobalRootDir, systemd.SystemMode, inter)

	if err := undoSnapdServicesOnCore(s, sysd); err != nil {
		return err
	}
	if err := undoSnapdUserServicesOnCore(s, inter); err != nil {
		return err
	}
	if err := undoSnapdToolingMountUnit(sysd); err != nil {
		return err
	}
	// XXX: reload after all operations?
	return nil
}
