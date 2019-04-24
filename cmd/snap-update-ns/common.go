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

type CommonProfileUpdateContext struct {
	// instanceName is the name of the snap instance to update.
	instanceName string

	currentProfilePath string
	desiredProfilePath string
}

// InstanceName returns the snap instance name being updated.
func (ctx *CommonProfileUpdateContext) InstanceName() string {
	return ctx.instanceName
}

func (ctx *CommonProfileUpdateContext) Lock() (unlock func(), err error) {
	return func() {}, nil
}

func (ctx *CommonProfileUpdateContext) Assumptions() *Assumptions {
	return nil
}

// LoadDesiredProfile loads the desired mount profile.
func (ctx *CommonProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(ctx.desiredProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load desired mount profile of snap %q: %s", ctx.instanceName, err)
	}
	return profile, nil
}

// LoadCurrentProfile loads the current mount profile.
func (ctx *CommonProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(ctx.currentProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load current mount profile of snap %q: %s", ctx.instanceName, err)
	}
	return profile, nil
}

// SaveCurrentProfile saves the current mount profile.
func (ctx *CommonProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if err := profile.Save(ctx.currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", ctx.instanceName, err)
	}
	return nil
}
