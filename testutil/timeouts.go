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

package testutil

import (
	"os"
	"runtime"
	"time"
)

var runtimeGOARCH = runtime.GOARCH

// HostScaledTimeout returns a timeout for tests that is adjusted
// for the slowness of certain systems.
//
// This should only be used in tests and is a bit of a guess.
func HostScaledTimeout(t time.Duration) time.Duration {
	switch {
	case runtimeGOARCH == "riscv64":
		// virt riscv64 builders are 5x times slower than
		// armhf when building golang-1.14. These tests
		// timeout, hence bump timeouts by 6x
		return t * 6
	case os.Getenv("GO_TEST_RACE") == "1":
		// the -race detector makes test execution time 2-20x slower
		return t * 5
	default:
		return t
	}
}
