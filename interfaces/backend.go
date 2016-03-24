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
	"github.com/ubuntu-core/snappy/snap"
)

// SecurityBackend abstracts interactions between the interface system and the
// needs of a particular security system.
type SecurityBackend interface {
	// ConfigureSnapSecurity creates and loads security artefacts specific to a
	// given snap. The snap can be in developer mode to make security violations
	// non-fatal to the offending application process.
	//
	// This method should be called after changing plug, slots, connections between
	// them or application present in the snap.
	ConfigureSnapSecurity(snapInfo *snap.Info, repo *Repository, developerMode bool) error

	// DeconfigureSnapSecurity removes security artefacts of a given snap.
	//
	// This method should be called after removing a snap.
	DeconfigureSnapSecurity(snapInfo *snap.Info) error

	// CommitDeferredChanges commits any buffered changes made so far.
	CommitDeferredChanges() error
}
