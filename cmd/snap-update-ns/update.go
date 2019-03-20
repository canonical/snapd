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

package main

import (
	"github.com/snapcore/snapd/osutil"
)

// MountProfileUpdateContext provides the context of a mount namespace update.
// The context provides a way to synchronize the operation with other users of
// the snap system, to load and save the mount profiles and to provide the file
// system assumptions with which the mount namespace will be modified.
type MountProfileUpdateContext interface {
	// Lock obtains locks appropriate for the update.
	Lock() (unlock func(), err error)
	// Assumptions computes filesystem assumptions under which the update shall operate.
	Assumptions() *Assumptions
	// LoadDesiredProfile loads the mount profile that should be constructed.
	LoadDesiredProfile() (*osutil.MountProfile, error)
	// LoadCurrentProfile loads the mount profile that is currently applied.
	LoadCurrentProfile() (*osutil.MountProfile, error)
	// SaveCurrentProfile saves the mount profile that is currently applied.
	SaveCurrentProfile(*osutil.MountProfile) error
}
