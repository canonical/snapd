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

package ifacetest

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// TestSecurityBackend is a security backend intended for testing.
type TestSecurityBackend struct {
	BackendName interfaces.SecuritySystem
	// SetupCalls stores information about all calls to Setup
	SetupCalls []TestSetupCall
	// RemoveCalls stores information about all calls to Remove
	RemoveCalls []string
	// SetupCallback is an callback that is optionally called in Setup
	SetupCallback func(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error
	// RemoveCallback is a callback that is optionally called in Remove
	RemoveCallback func(snapName string) error
	// SandboxFeaturesCallback is a callback that is optionally called in SandboxFeatures
	SandboxFeaturesCallback func() []string
}

// TestSetupCall stores details about calls to TestSecurityBackend.Setup
type TestSetupCall struct {
	// SnapInfo is a copy of the snapInfo argument to a particular call to Setup
	SnapInfo *snap.Info
	// Options is a copy of the confinement options to a particular call to Setup
	Options interfaces.ConfinementOptions
}

// Initialize does nothing.
func (b *TestSecurityBackend) Initialize() error {
	return nil
}

// Name returns the name of the security backend.
func (b *TestSecurityBackend) Name() interfaces.SecuritySystem {
	return b.BackendName
}

// Setup records information about the call and calls the setup callback if one is defined.
func (b *TestSecurityBackend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	b.SetupCalls = append(b.SetupCalls, TestSetupCall{SnapInfo: snapInfo, Options: opts})
	if b.SetupCallback == nil {
		return nil
	}
	return b.SetupCallback(snapInfo, opts, repo)
}

// Remove records information about the call and calls the remove callback if one is defined
func (b *TestSecurityBackend) Remove(snapName string) error {
	b.RemoveCalls = append(b.RemoveCalls, snapName)
	if b.RemoveCallback == nil {
		return nil
	}
	return b.RemoveCallback(snapName)
}

func (b *TestSecurityBackend) NewSpecification() interfaces.Specification {
	return &Specification{}
}

func (b *TestSecurityBackend) SandboxFeatures() []string {
	if b.SandboxFeaturesCallback == nil {
		return nil
	}
	return b.SandboxFeaturesCallback()
}
