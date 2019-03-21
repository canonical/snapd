// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main_test

import (
	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
)

// testProfileUpdateContext implements MountProfileUpdate and is suitable for testing.
type testProfileUpdateContext struct {
	loadCurrentProfile func() (*osutil.MountProfile, error)
	loadDesiredProfile func() (*osutil.MountProfile, error)
	saveCurrentProfile func(*osutil.MountProfile) error
	assumptions        func() *update.Assumptions
}

func (up *testProfileUpdateContext) Lock() (unlock func(), err error) {
	return func() {}, nil
}

func (up *testProfileUpdateContext) Assumptions() *update.Assumptions {
	if up.assumptions != nil {
		return up.assumptions()
	}
	return &update.Assumptions{}
}

func (up *testProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	if up.loadCurrentProfile != nil {
		return up.loadCurrentProfile()
	}
	return &osutil.MountProfile{}, nil
}

func (up *testProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	if up.loadDesiredProfile != nil {
		return up.loadDesiredProfile()
	}
	return &osutil.MountProfile{}, nil
}

func (up *testProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if up.saveCurrentProfile != nil {
		return up.saveCurrentProfile(profile)
	}
	return nil
}
