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

	"github.com/snapcore/snapd/boot"
)

type mockDevice struct {
	str  string
	uc20 bool
}

// MockDevice implements boot.Device. It wraps a string like
// <boot-snap-name>[@<mode>], no <boot-snap-name> means classic, no
// <mode> defaults to "run". It returns <boot-snap-name> for both
// Base and Kernel, for more control mock a DeviceContext.
func MockDevice(s string) boot.Device {
	return &mockDevice{str: s}
}

// MockUC20Device implements boot.Device and returns true for HasModeenv.
func MockUC20Device(s string) boot.Device {
	return &mockDevice{str: s, uc20: true}
}

func (d *mockDevice) snapAndMode() []string {
	parts := strings.SplitN(string(d.str), "@", 2)
	if len(parts) == 1 {
		return append(parts, "run")
	}
	if parts[1] == "" {
		return []string{parts[0], "run"}
	}
	return parts
}

func (d *mockDevice) Kernel() string { return d.snapAndMode()[0] }
func (d *mockDevice) Base() string   { return d.snapAndMode()[0] }
func (d *mockDevice) Classic() bool  { return d.snapAndMode()[0] == "" }
func (d *mockDevice) RunMode() bool  { return d.snapAndMode()[1] == "run" }

// HasModeenv is true when created with uc20 string.
func (d *mockDevice) HasModeenv() bool {
	return d.uc20
}
