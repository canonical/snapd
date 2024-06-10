// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

// KernelSnapInfo includes information from the kernel snap that is
// needed to build a drivers tree. Defin
type KernelSnapInfo struct {
	Name     string
	Revision snap.Revision
	// MountPoint is the root of the files from the kernel snap
	MountPoint string
	// NeedsDriversTree will be set if a drivers tree needs to be
	// build on installation
	NeedsDriversTree bool
	// IsCore is set if this is UC
	IsCore bool
}
