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

// Package preseed provides functions for preseeding of classic and UC20
// systems. Preseeding runs snapd in special mode that executes significant
// portion of initial seeding in a chroot environment and stores the resulting
// modifications in the image so that they can be reused and skipped on first boot,
// speeding it up.
package preseed

import (
	"io"
	"os"
)

var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

type preseedOpts struct {
	PrepareImageDir  string
	PreseedChrootDir string
	SystemLabel      string
	WritableDir      string

	// optional path to AppArmor kernel features directory
	AppArmorKernelFeaturesDir string
}

type targetSnapdInfo struct {
	path    string
	version string
}
