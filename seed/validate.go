// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package seed

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/timings"
)

type ValidationError struct {
	// SystemErrors maps system labels ("" for UC16/18) to their validation errors.
	SystemErrors map[string][]error
}

func newValidationError(label string, err error) *ValidationError {
	return &ValidationError{SystemErrors: map[string][]error{
		label: {err},
	}}
}

func (e *ValidationError) addErr(label string, errs ...error) {
	if e.SystemErrors == nil {
		e.SystemErrors = make(map[string][]error)
	}
	e.SystemErrors[label] = append(e.SystemErrors[label], errs...)
}

func (e ValidationError) hasErrors() bool {
	return len(e.SystemErrors) != 0
}

func (e *ValidationError) Error() string {
	systems := make([]string, 0, len(e.SystemErrors))
	for s := range e.SystemErrors {
		systems = append(systems, s)
	}
	sort.Strings(systems)
	var buf bytes.Buffer
	first := true
	for _, s := range systems {
		if first {
			if s == "" {
				fmt.Fprintf(&buf, "cannot validate seed:")
			} else {
				fmt.Fprintf(&buf, "cannot validate seed system %q:", s)
			}
		} else {
			fmt.Fprintf(&buf, "\nand seed system %q:", s)
		}
		for _, err := range e.SystemErrors[s] {
			fmt.Fprintf(&buf, "\n - %s", err)
		}
		first = false
	}
	return buf.String()
}

// ValidateFromYaml validates the given seed.yaml file and surrounding seed.
func ValidateFromYaml(seedYamlFile string) error {
	// TODO:UC20: support validating also one or multiple UC20 seed systems
	// introduce ListSystems ?
	// What about full empty seed dir?
	seedDir := filepath.Dir(seedYamlFile)

	seed, err := Open(seedDir, "")
	if err != nil {
		return newValidationError("", err)
	}

	if err := seed.LoadAssertions(nil, nil); err != nil {
		return newValidationError("", err)
	}

	tm := timings.New(nil)
	if err := seed.LoadMeta(AllModes, nil, tm); err != nil {
		if missingErr, ok := err.(*essentialSnapMissingError); ok {
			if seed.Model().Classic() && missingErr.SnapName == "core" {
				err = fmt.Errorf("essential snap core or snapd must be part of the seed")
			}
		}
		return newValidationError("", err)
	}

	ve := &ValidationError{}
	// read the snap infos
	snapInfos := make([]*snap.Info, 0, seed.NumSnaps())
	seed.Iter(func(sn *Snap) error {
		snapf, err := snapfile.Open(sn.Path)
		if err != nil {
			ve.addErr("", err)
		} else {
			info, err := snap.ReadInfoFromSnapFile(snapf, sn.SideInfo)
			if err != nil {
				ve.addErr("", fmt.Errorf("cannot use snap %q: %v", sn.Path, err))
			} else {
				snapInfos = append(snapInfos, info)
			}
		}
		return nil
	})

	// TODO: surface the warnings too, like we do for snap container checks
	if _, errs2 := snap.ValidateBasesAndProviders(snapInfos); errs2 != nil {
		ve.addErr("", errs2...)
	}
	if ve.hasErrors() {
		return ve
	}

	return nil
}
