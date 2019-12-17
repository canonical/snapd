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

package boottest

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/snap"
)

// ensure MockBootParticipant is a BootParticipant
var _ boot.BootParticipant = &MockBootParticipant{}

// ensure MockBootParticipant is a Kernel
var _ boot.BootKernel = &MockBootParticipant{}

type MockBootParticipant struct {
	SetNextBootCalled         int
	ChangeRequiresRebootValue bool

	ExtractKernelAssetsCalled int
	ExtractKernelAssetsErr    error
	RemoveKernelAssetsCalled  int
	RemoveKernelAssetsErr     error
}

func (m *MockBootParticipant) SetNextBoot() error {
	m.SetNextBootCalled++
	return nil
}

func (m *MockBootParticipant) ChangeRequiresReboot() bool {
	return m.ChangeRequiresRebootValue
}

func (m *MockBootParticipant) IsTrivial() bool {
	return false
}

func (m *MockBootParticipant) RemoveKernelAssets() error {
	m.RemoveKernelAssetsCalled++
	return m.RemoveKernelAssetsErr
}

func (m *MockBootParticipant) ExtractKernelAssets(snap.Container) error {
	m.ExtractKernelAssetsCalled++
	return m.ExtractKernelAssetsErr
}
