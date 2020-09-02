// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install

import (
	"github.com/snapcore/snapd/asserts"
)

type Options struct {
	// Also mount the filesystems after creation
	Mount bool
	// Encrypt the data partition
	Encrypt bool
	// KernelPath is the path to the kernel to seal the keyfile to
	KernelPath string
	// Model is the device model to seal the keyfile to
	Model *asserts.Model
	// SystemLabel is the recover system label to seal the keyfile to
	SystemLabel string
}
