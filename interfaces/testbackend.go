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

package interfaces

import (
	"github.com/ubuntu-core/snappy/snap"
)

// TestSecurityBackend is a security backend intended for testing.
type TestSecurityBackend struct {
	// SetupCalls stores information about all calls to Setup
	SetupCalls []TestSetupCall
	// RemoveCalls stores information about all calls to Remove
	RemoveCalls []string
	// SetupCallback is an callback that is optionally called in Setup
	SetupCallback func(snapInfo *snap.Info, developerMode bool, repo *Repository) error
	// RemoveCallback is a callback that is optionally called in Remove
	RemoveCallback func(snapName string) error
}

// TestSetupCall stores details about calls to TestSecurityBackend.Setup
type TestSetupCall struct {
	// SnapInfo is a copy of the snapInfo argument to a particular call to Setup
	SnapInfo *snap.Info
	// DevMode is a copy of the developerMode argument to a particular call to Setup
	DevMode bool
}

// Name returns the name of the security backend.
func (b *TestSecurityBackend) Name() string {
	return "test"
}

// Setup records information about the call and calls the setup callback if one is defined.
func (b *TestSecurityBackend) Setup(snapInfo *snap.Info, devMode bool, repo *Repository) error {
	b.SetupCalls = append(b.SetupCalls, TestSetupCall{SnapInfo: snapInfo, DevMode: devMode})
	if b.SetupCallback == nil {
		return nil
	}
	return b.SetupCallback(snapInfo, devMode, repo)
}

// Remove records information about the call and calls the remove callback if one is defined
func (b *TestSecurityBackend) Remove(snapName string) error {
	b.RemoveCalls = append(b.RemoveCalls, snapName)
	if b.RemoveCallback == nil {
		return nil
	}
	return b.RemoveCallback(snapName)
}
