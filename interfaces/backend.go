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

// SecurityBackend abstracts interactions between the interface system and the
// needs of a particular security system.
type SecurityBackend interface {
	// Name returns the name of the backend.
	// This is intended for diagnostic messages.
	Name() string

	// Setup creates and loads security artefacts specific to a given snap.
	// The snap can be in developer mode to make security violations non-fatal
	// to the offending application process.
	//
	// This method should be called after changing plug, slots, connections
	// between them or application present in the snap.
	Setup(snapInfo *snap.Info, devMode bool, repo *Repository) error

	// Remove removes and unloads security artefacts of a given snap.
	//
	// This method should be called during the process of removing a snap.
	Remove(snapName string) error
}
