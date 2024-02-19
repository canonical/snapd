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
func (b *TestSecurityBackend) Initialize(*interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the security backend.
func (b *TestSecurityBackend) Name() interfaces.SecuritySystem {
	return b.BackendName
}

// Setup records information about the call and calls the setup callback if one is defined.
func (b *TestSecurityBackend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
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

func (b *TestSecurityBackend) NewSpecification(*interfaces.SnapAppSet) interfaces.Specification {
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
	SetupManyCallback func(snapInfo []*snap.Info, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error
}

// TestSetupManyCall stores details about calls to TestSecurityBackendMany.SetupMany
type TestSetupManyCall struct {
	// SnapInfos is a copy of the snapInfo arguments to a particular call to SetupMany
	SnapInfos []*snap.Info
}

func (b *TestSecurityBackendSetupMany) SetupMany(snaps []*snap.Info, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
	b.SetupManyCalls = append(b.SetupManyCalls, TestSetupManyCall{SnapInfos: snaps})
	if b.SetupManyCallback == nil {
		return nil
	}
	return b.SetupManyCallback(snaps, confinement, repo, tm)
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
