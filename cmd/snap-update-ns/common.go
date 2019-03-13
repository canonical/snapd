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

type CommonProfileUpdate struct{}

func (up *CommonProfileUpdate) Lock() (unlock func(), err error) {
	return func() {}, nil
}

func (up *CommonProfileUpdate) Assumptions() *Assumptions {
	return nil
}

// NeededChanges computes the sequence of mount changes needed to transform current profile to desired profile.
func (up *CommonProfileUpdate) NeededChanges(current, desired *osutil.MountProfile) []*Change {
	return NeededChanges(current, desired)
}

// PerformChange performs a given mount namespace change under given filesystem assumptions.
func (up *CommonProfileUpdate) PerformChange(change *Change, as *Assumptions) ([]*Change, error) {
	return changePerform(change, as)
}

func (up *CommonProfileUpdate) LoadDesiredProfile() (*osutil.MountProfile, error) {
	return nil, nil
}

func (up *CommonProfileUpdate) LoadCurrentProfile() (*osutil.MountProfile, error) {
	return nil, nil
}

func (up *CommonProfileUpdate) SaveCurrentProfile(profile *osutil.MountProfile) error {
	return nil
}
