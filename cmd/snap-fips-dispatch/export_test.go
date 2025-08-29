// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

// A dispatcher for bootstrapping FIPS environment. It is expected to be
// symlinked as /usr/bin/snap,
// /usr/lib/snapd/{snapd,snap-repair,snap-bootstrap}.
//
// The dispatcher sets up the environment by expliclty enabling FIPS support
// (through GOFIPS=1), and injects environment variables such that the Go FIPS
// toolchain runtime can locate the relevant OpenSSL FIPS provider module.
package main

import (
	"github.com/snapcore/snapd/testutil"
)

var (
	Run = run
)

func MockSnapdtoolDispatchWithFIPS(m func(target string) error) (restore func()) {
	return testutil.Mock(&snapdtoolDispatchWithFIPS, m)
}
