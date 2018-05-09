// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package interfaces

import (
	"github.com/snapcore/snapd/snap"
)

// ConfinementOptions describe confinement configuration.
//
// The confinement system controls the initial layout of the mount namespace as
// well as the set of actions a process is allowed to perform. Confinement is
// initially defined by the ConfinementType declared by the snap. It can be
// either "strict", "devmode" or "classic".
//
// The "strict" type uses mount layout that puts the core snap as the root
// filesystem and provides strong isolation from the system and from other
// snaps. Violations cause permission errors or mandatory process termination.
//
// The "devmode" type uses the same mount layout as "strict" but switches
// confinement to non-enforcing mode whenever possible. Violations that would
// result in permission error or process termination are instead permitted. A
// diagnostic message is logged when this occurs.
//
// The "classic" type uses mount layout that is identical to the runtime of the
// classic system snapd runs in, in other words there is no "chroot". Most of
// the confinement is lifted, specifically there's no seccomp filter being
// applied and apparmor is using complain mode by default.
//
// The three types defined above map to some combinations of the three flags
// defined below.
//
// The DevMode flag attempts to switch all confinement facilities into
// non-enforcing mode even if the snap requested otherwise.
//
// The JailMode flag attempts to switch all confinement facilities into
// enforcing mode even if the snap requested otherwise.
//
// The Classic flag switches the layout of the mount namespace so that there's
// no "chroot" to the core snap.
type ConfinementOptions struct {
	// DevMode flag switches confinement to non-enforcing mode.
	DevMode bool
	// JailMode flag switches confinement to enforcing mode.
	JailMode bool
	// Classic flag switches the core snap "chroot" off.
	Classic bool
}

// SecurityBackend abstracts interactions between the interface system and the
// needs of a particular security system.
type SecurityBackend interface {
	// Initialize performs any initialization required by the backend.
	// It is called during snapd startup process.
	Initialize() error

	// Name returns the name of the backend.
	// This is intended for diagnostic messages.
	Name() SecuritySystem

	// Setup creates and loads security artefacts specific to a given snap.
	// The snap can be in one of three kids onf confinement (strict mode,
	// developer mode or classic mode). In the last two security violations
	// are non-fatal to the offending application process.
	//
	// This method should be called after changing plug, slots, connections
	// between them or application present in the snap.
	Setup(snapInfo *snap.Info, opts ConfinementOptions, repo *Repository) error

	// Remove removes and unloads security artefacts of a given snap.
	//
	// This method should be called during the process of removing a snap.
	Remove(snapName string) error

	// NewSpecification returns a new specification associated with this backend.
	NewSpecification() Specification

	// SandboxFeatures returns a list of tags that identify sandbox features.
	SandboxFeatures() []string
}
