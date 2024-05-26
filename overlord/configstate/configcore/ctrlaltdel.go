// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package configcore

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

// Systemd unit name to control ctrl-alt-del behaviour
const (
	ctrlAltDelTarget = "ctrl-alt-del.target"
)

// Supported actions for core.system.ctrl-alt-del-action
const (
	ctrlAltDelReboot = "reboot"
	ctrlAltDelNone   = "none"
)

func init() {
	supportedConfigurations["core.system.ctrl-alt-del-action"] = true
}

type sysdCtrlAltDelLogger struct{}

func (l *sysdCtrlAltDelLogger) Notify(status string) {
	fmt.Fprintf(Stderr, "ctrl-alt-del: %s\n", status)
}

// switchCtrlAltDelAction configures the systemd handling of the special
// ctrl-alt-del keyboard sequence. This function supports configuring
// systemd to trigger a reboot, or ignore the key sequence.
func switchCtrlAltDelAction(action string, opts *fsOnlyContext) error {
	if action != "reboot" && action != "none" {
		return fmt.Errorf("invalid action %q supplied for system.ctrl-alt-del-action option", action)
	}

	// The opts argument tells us if the real rootfs is mounted and
	// systemd is running normally.
	// opts != nil: The direct rootfs path is supplied because the system is not
	//              ready so we cannot use systemctl to modify unit files
	//              without supplying the root path.
	// opts == nil: No rootfs path is supplied because we are running with
	//              rootfs mounted, so we can use systemctl normally. This
	//              case is used for normal runtime changes.
	var sysd systemd.Systemd
	if opts != nil {
		// Use systemctl for direct unit file manipulations (support a
		// subset of unit operations such as enable/disable/mark/unmask)
		// See: "systemctl --root=<dir>"
		sysd = systemd.NewEmulationMode(opts.RootDir)
	} else {
		// Use systemctl with access to all the unit operations
		sysd = systemd.New(systemd.SystemMode, &sysdCtrlAltDelLogger{})

		// Make sure the ctrl-alt-del.target unit is in the expected state.
		// (1) The required unit should be present (file exist under /{run,etc,lib}/systemd/system).
		// (2) The Enable state for a unit typically means automatic startup on boot. The
		//     expected state for reboot.target (ctrl-alt-del.target) is 'disabled'.
		//     The ctrl-alt-del.target unit is an alias for reboot.target. This means that
		//     if reboot.target is enabled, a ctrl-alt-del.target symlink will be created
		//     under /etc/systemd/system, which is not needed and will prevent masking
		status := mylog.Check2(sysd.Status([]string{ctrlAltDelTarget}))

		switch {
		case len(status) != 1:
			return fmt.Errorf("internal error: expected status of target %s, got %v", ctrlAltDelTarget, status)
		case !status[0].Installed:
			return fmt.Errorf("internal error: target %s not installed", ctrlAltDelTarget)
		case status[0].Enabled:
			return fmt.Errorf("internal error: target %s should not be enabled", ctrlAltDelTarget)
		}
	}

	// It is safe to mask an already masked unit, and unmask an already unmasked unit. The code
	// will not try to optimize this because this will require knowledge of the initial
	// "unset" state, which complicates the problem unnecessarily, for not much benefit.
	switch action {
	case ctrlAltDelNone:
		mylog.Check(
			// the unit is masked and cannot be started
			sysd.Mask(ctrlAltDelTarget))

	case ctrlAltDelReboot:
		mylog.Check(
			// the unit is no longer masked and thus can be started on demand causing a reboot
			sysd.Unmask(ctrlAltDelTarget))

	default:
		// We already checked the action against the list of defined actions. This is an
		// internal double check to see if any of the actions are unhandled with in this switch.
		return fmt.Errorf("internal error: action %s unhandled on this platform", action)
	}
	return nil
}

func handleCtrlAltDelConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	output := mylog.Check2(coreCfg(tr, "system.ctrl-alt-del-action"))

	// The coreCfg() function returns an empty string ("") if
	// the config key was unset (not found). We only react on
	// explicitly set actions.
	if output != "" {
		mylog.Check(switchCtrlAltDelAction(output, opts))
	}
	return nil
}
