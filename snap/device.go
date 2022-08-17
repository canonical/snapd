// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package snap

import "github.com/snapcore/snapd/asserts"

// Device carries information about the device model and mode that is
// relevant to boot and other packages. Note snapstate.DeviceContext implements
// this, and that's the expected use case.
type Device interface {
	RunMode() bool
	Classic() bool

	Kernel() string
	Base() string
	Gadget() string

	HasModeenv() bool
	IsCoreBoot() bool    // true if UC or classic with modes
	IsClassicBoot() bool // true if classic with classic initramfs

	Model() *asserts.Model
}
