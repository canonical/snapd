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
	"fmt"

	"github.com/snapcore/snapd/osutil"
)

type CommonProfileUpdate struct {
	// instanceName is the name of the snap or instance to update.
	instanceName string

	currentProfilePath string
	desiredProfilePath string
}

func (up *CommonProfileUpdate) Lock() (unlock func(), err error) {
	return func() {}, nil
}

func (up *CommonProfileUpdate) Assumptions() *Assumptions {
	return nil
}

// LoadDesiredProfile loads the desired mount profile.
func (up *CommonProfileUpdate) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(up.desiredProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load desired mount profile of snap %q: %s", up.instanceName, err)
	}
	return profile, nil
}

// LoadCurrentProfile loads the current mount profile.
func (up *CommonProfileUpdate) LoadCurrentProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(up.currentProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load current mount profile of snap %q: %s", up.instanceName, err)
	}
	return profile, nil
}

// SaveCurrentProfile saves the current mount profile.
func (up *CommonProfileUpdate) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if err := profile.Save(up.currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", up.instanceName, err)
	}
	return nil
}
