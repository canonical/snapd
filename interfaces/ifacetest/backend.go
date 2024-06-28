// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"github.com/snapcore/snapd/timings"
)

// TestSecurityBackend is a security backend intended for testing.
type TestSecurityBackend struct {
	BackendName interfaces.SecuritySystem
	// SetupCalls stores information about all calls to Setup
	SetupCalls []TestSetupCall
	// RemoveCalls stores information about all calls to Remove
	RemoveCalls []string
	// SetupCallback is an callback that is optionally called in Setup
	SetupCallback func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error
	// RemoveCallback is a callback that is optionally called in Remove
	RemoveCallback func(snapName string) error
	// SandboxFeaturesCallback is a callback that is optionally called in SandboxFeatures
	SandboxFeaturesCallback func() []string
}

// TestSetupCall stores details about calls to TestSecurityBackend.Setup
type TestSetupCall struct {
	// AppSet is a copy of the appSet argument to a particular call to Setup
	AppSet *interfaces.SnapAppSet
	// Options is a copy of the confinement options to a particular call to Setup
	Options interfaces.ConfinementOptions
}

// Initialize does nothing.
func (b *TestSecurityBackend) Initialize(*interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the security backend.
func (b *TestSecurityBackend) Name() interfaces.SecuritySystem {
	return b.BackendName
}

// Setup records information about the call and calls the setup callback if one is defined.
func (b *TestSecurityBackend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	b.SetupCalls = append(b.SetupCalls, TestSetupCall{AppSet: appSet, Options: opts})
	if b.SetupCallback == nil {
		return nil
	}
	return b.SetupCallback(appSet, opts, repo)
}

// Remove records information about the call and calls the remove callback if one is defined
func (b *TestSecurityBackend) Remove(snapName string) error {
	b.RemoveCalls = append(b.RemoveCalls, snapName)
	if b.RemoveCallback == nil {
		return nil
	}
	return b.RemoveCallback(snapName)
}

func (b *TestSecurityBackend) NewSpecification(*interfaces.SnapAppSet, interfaces.ConfinementOptions) interfaces.Specification {
	return &Specification{}
}

func (b *TestSecurityBackend) SandboxFeatures() []string {
	if b.SandboxFeaturesCallback == nil {
		return nil
	}
	return b.SandboxFeaturesCallback()
}

// TestSecurityBackendSetupMany is a security backend that implements SetupMany on top of TestSecurityBackend.
type TestSecurityBackendSetupMany struct {
	TestSecurityBackend

	// SetupManyCalls stores information about all calls to Setup
	SetupManyCalls []TestSetupManyCall

	// SetupManyCallback is an callback that is optionally called in Setup
	SetupManyCallback func(appSets []*interfaces.SnapAppSet, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error
}

// TestSetupManyCall stores details about calls to TestSecurityBackendMany.SetupMany
type TestSetupManyCall struct {
	// AppSets is a copy of the appSets arguments to a particular call to SetupMany
	AppSets []*interfaces.SnapAppSet
}

func (b *TestSecurityBackendSetupMany) SetupMany(appSets []*interfaces.SnapAppSet, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
	b.SetupManyCalls = append(b.SetupManyCalls, TestSetupManyCall{AppSets: appSets})
	if b.SetupManyCallback == nil {
		return nil
	}
	return b.SetupManyCallback(appSets, confinement, repo, tm)
}

// TestSecurityBackendDiscardingLate implements RemoveLate on top of TestSecurityBackend.
type TestSecurityBackendDiscardingLate struct {
	TestSecurityBackend

	RemoveLateCalledFor [][]string
	RemoveLateCallback  func(snapName string, rev snap.Revision, typ snap.Type) error
}

func (b *TestSecurityBackendDiscardingLate) RemoveLate(snapName string, rev snap.Revision, typ snap.Type) error {
	b.RemoveLateCalledFor = append(b.RemoveLateCalledFor, []string{
		snapName, rev.String(), string(typ),
	})
	if b.RemoveLateCallback == nil {
		return nil
	}
	return b.RemoveLateCallback(snapName, rev, typ)
}
