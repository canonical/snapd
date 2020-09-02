// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package secboot

// This file must not have a build-constraint and must not import
// the github.com/snapcore/secboot repository. That will ensure
// it can be build as part of the debian build without secboot.
// Debian does run "go list" without any support for passing -tags.

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
)

type SealKeyModelParams struct {
	// The snap model
	Model *asserts.Model
	// The set of EFI binary load paths for the current device configuration
	EFILoadChains [][]bootloader.BootFile
	// The kernel command line
	KernelCmdlines []string
}

type SealKeyParams struct {
	// The parameters we're sealing the key to
	ModelParams []*SealKeyModelParams
	// The path to store the sealed key file
	KeyFile string
	// The path to authorization policy update data file (only relevant for TPM)
	TPMPolicyUpdateDataFile string
	// The path to the lockout authorization file (only relevant for TPM)
	TPMLockoutAuthFile string
}
