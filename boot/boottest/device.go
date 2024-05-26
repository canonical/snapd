// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
)

type mockDevice struct {
	bootSnap  string
	mode      string
	hasModes  bool
	isClassic bool

	model *asserts.Model
}

// MockDevice implements boot.Device. It wraps a string like
// <boot-snap-name>[@<mode>], no <boot-snap-name> means classic, empty
// <mode> defaults to "run" for UC16/18. If mode is set HasModeenv
// returns true for UC20 and an empty boot snap name panics.
// It returns <boot-snap-name> for Base, Kernel and gadget, for more
// control mock a DeviceContext.
func MockDevice(s string) snap.Device {
	bootsnap, mode, uc20 := snapAndMode(s)
	if uc20 && bootsnap == "" {
		panic("MockDevice with no snap name and @mode is unsupported")
	}
	return &mockDevice{
		bootSnap:  bootsnap,
		mode:      mode,
		hasModes:  uc20,
		isClassic: bootsnap == "",
	}
}

// mockDeviceWithModes implements boot.Device and returns true for
// HasModeenv.  Arguments are mode (empty means "run"), model, and a
// boolean specifying if this is a classic with modes or a UC device.
// If model is nil a default model is used (MakeMockUC20Model or
// MakeMockClassicWithModesModel is called).
func mockDeviceWithModes(mode string, model *asserts.Model, isClassic bool) snap.Device {
	if mode == "" {
		mode = "run"
	}
	if model == nil {
		if isClassic {
			model = MakeMockClassicWithModesModel()
		} else {
			model = MakeMockUC20Model()
		}
	}
	return &mockDevice{
		bootSnap:  model.Kernel(),
		mode:      mode,
		hasModes:  true,
		isClassic: isClassic,
		model:     model,
	}
}

// MockUC20Device mocks a UC with modes device.
// Arguments are mode (empty means "run"), and model.
func MockUC20Device(mode string, model *asserts.Model) snap.Device {
	if model != nil && model.Classic() {
		panic("MockUC20Device called with classic model")
	}
	isClassic := false
	return mockDeviceWithModes(mode, model, isClassic)
}

// MockClassicWithModesDevice mocks a classic with modes device.
// Arguments are mode (empty means "run"), and model.
func MockClassicWithModesDevice(mode string, model *asserts.Model) snap.Device {
	if model != nil && !model.Classic() {
		panic("MockClassicWithModesDevice called with Ubuntu Core model")
	}
	isClassic := true
	return mockDeviceWithModes(mode, model, isClassic)
}

func snapAndMode(str string) (snap, mode string, uc20 bool) {
	parts := strings.SplitN(string(str), "@", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "run", false
	}
	return parts[0], parts[1], true
}

func (d *mockDevice) Kernel() string   { return d.bootSnap }
func (d *mockDevice) Classic() bool    { return d.isClassic }
func (d *mockDevice) RunMode() bool    { return d.mode == "run" }
func (d *mockDevice) HasModeenv() bool { return d.hasModes }
func (d *mockDevice) IsCoreBoot() bool {
	if d.model != nil {
		return d.model.Kernel() != ""
	}
	return d.hasModes || !d.isClassic
}
func (d *mockDevice) IsClassicBoot() bool { return !d.IsCoreBoot() }
func (d *mockDevice) Base() string {
	if d.model != nil {
		return d.model.Base()
	}
	return d.bootSnap
}

func (d *mockDevice) Gadget() string {
	if d.model != nil {
		return d.model.Gadget()
	}
	return d.bootSnap
}

func (d *mockDevice) Model() *asserts.Model {
	if d.model == nil {
		panic("Device.Model called but MockUC20Device not used")
	}
	return d.model
}
