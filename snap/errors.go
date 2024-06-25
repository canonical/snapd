// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap

import (
	"fmt"
)

type AlreadyInstalledError struct {
	Snap string
}

func (e AlreadyInstalledError) Error() string {
	return fmt.Sprintf("snap %q is already installed", e.Snap)
}

type NotInstalledError struct {
	Snap string
	Rev  Revision
}

func (e NotInstalledError) Error() string {
	if e.Rev.Unset() {
		return fmt.Sprintf("snap %q is not installed", e.Snap)
	}
	return fmt.Sprintf("revision %s of snap %q is not installed", e.Rev, e.Snap)
}

func (e *NotInstalledError) Is(err error) bool {
	_, ok := err.(*NotInstalledError)
	return ok
}

// NotSnapError is returned if an operation expects a snap file or snap dir
// but no valid input is provided. When creating it ensure "Err" is set
// so that a useful error can be displayed to the user.
type NotSnapError struct {
	Path string

	Err error
}

func (e NotSnapError) Error() string {
	// e.Err should always be set but support if not
	if e.Err == nil {
		return fmt.Sprintf("cannot process snap or snapdir %q", e.Path)
	}
	return fmt.Sprintf("cannot process snap or snapdir: %v", e.Err)
}

// ComponentNotInstalledError is used when a component is not in the
// system while trying to manage it.
type ComponentNotInstalledError struct {
	NotInstalledError
	Component string
	CompRev   Revision
}

func (e ComponentNotInstalledError) Error() string {
	if e.CompRev.Unset() {
		return fmt.Sprintf("component %q is not installed for revision %s of snap %q",
			e.Component, e.Rev, e.Snap)
	}
	return fmt.Sprintf("revision %s of component %q is not installed for revision %s of snap %q",
		e.CompRev, e.Component, e.Rev, e.Snap)
}
