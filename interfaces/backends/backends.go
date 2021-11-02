// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package backends

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
)

var All []interfaces.SecurityBackend = backends()

func backends() []interfaces.SecurityBackend {
	all := []interfaces.SecurityBackend{
		// Because of how the GPIO interface is implemented the systemd backend
		// must be earlier in the sequence than the apparmor backend.
		&systemd.Backend{},
		&seccomp.Backend{},
		&dbus.Backend{},
		&udev.Backend{},
		&mount.Backend{},
		&kmod.Backend{},
		&polkit.Backend{},
	}

	// TODO use something like:
	// level, summary := apparmor.ProbeResults()

	// This should be logger.Noticef but due to ordering of initialization
	// calls, the logger is not ready at this point yet and the message goes
	// nowhere. Per advice from other snapd developers, we just print it
	// directly.
	//
	// TODO: on this should become a user-visible message via the user-warning
	// framework, so that users are aware that we have non-strict confinement.
	// By printing this directly we ensure it will end up the journal for the
	// snapd.service. This aspect should be retained even after the switch to
	// user-warning.
	fmt.Printf("AppArmor status: %s\n", apparmor_sandbox.Summary())

	// Enable apparmor backend if there is any level of apparmor support,
	// including partial feature set. This will allow snap-confine to always
	// link to apparmor and check if it is enabled on boot, knowing that there
	// is always *some* profile to apply to each snap process.
	//
	// When some features are missing the backend will generate more permissive
	// profiles that keep applications operational, in forced-devmode.
	switch apparmor_sandbox.ProbedLevel() {
	case apparmor_sandbox.Partial, apparmor_sandbox.Full:
		all = append(all, &apparmor.Backend{})
	}
	return all
}
