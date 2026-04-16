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
	"sort"
	"strings"
)

// Should not construct this error directly. Use NewAlreadyInstalledSnapsError,
// NewAlreadyInstalledComponentsError, or NewAlreadyInstalledError.
type AlreadyInstalledError struct {
	Snaps      []string
	Components map[string][]string
}

func (e AlreadyInstalledError) Error() string {
	var comps []string
	for snap, components := range e.Components {
		for _, comp := range components {
			comps = append(comps, SnapComponentName(snap, comp))
		}
	}
	sort.Strings(comps)

	builder := strings.Builder{}
	if len(e.Snaps) == 1 {
		fmt.Fprintf(&builder, "snap %q ", e.Snaps[0])
	} else if len(e.Snaps) > 1 {
		fmt.Fprintf(&builder, "snaps %q ", strings.Join(e.Snaps, ","))
	}

	if len(e.Snaps) > 0 && len(comps) > 0 {
		fmt.Fprintf(&builder, "and ")
	}

	if len(comps) == 1 {
		fmt.Fprintf(&builder, "component %q ", comps[0])
	} else if len(comps) > 1 {
		fmt.Fprintf(&builder, "components %q ", strings.Join(comps, ","))
	}

	if len(e.Snaps)+len(comps) > 1 {
		fmt.Fprintf(&builder, "are already installed")
	} else {
		fmt.Fprintf(&builder, "is already installed")
	}

	return builder.String()
}

func (e AlreadyInstalledError) Is(err error) bool {
	other, ok := err.(AlreadyInstalledError)
	if !ok {
		return false
	}

	if !slicesEqual(e.Snaps, other.Snaps) {
		return false
	}

	if len(e.Components) != len(other.Components) {
		return false
	}

	for snap, comps := range e.Components {
		otherComps, ok := other.Components[snap]
		if !ok || !slicesEqual(comps, otherComps) {
			return false
		}
	}
	return true
}

// slicesEqual is a helper function to compare two slices for equality.
// TODO:GOVERSION 1.21: replace with slices.Equal
func slicesEqual[S []E, E comparable](a, b S) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func NewAlreadyInstalledSnapsError(snaps []string) *AlreadyInstalledError {
	return NewAlreadyInstalledError(snaps, nil)
}

func NewAlreadyInstalledComponentsError(snapName string, comps []string) *AlreadyInstalledError {
	return NewAlreadyInstalledError(nil, map[string][]string{
		snapName: comps,
	})
}

func NewAlreadyInstalledError(snaps []string, comps map[string][]string) *AlreadyInstalledError {
	// sort snaps for use with .Is()
	if len(snaps) > 0 {
		sort.Strings(snaps)
	}

	if len(comps) > 0 {
		// sort components for use with .Is()
		for _, scomps := range comps {
			if len(scomps) > 0 {
				sort.Strings(scomps)
			}
		}
	}

	return &AlreadyInstalledError{
		Snaps:      snaps,
		Components: comps,
	}
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
