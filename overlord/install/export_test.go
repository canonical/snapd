// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/sysconfig"
)

var CheckFDEFeatures = checkFDEFeatures

func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}

func MockSysconfigConfigureTargetSystem(f func(mod *asserts.Model, opts *sysconfig.Options) error) (restore func()) {
	old := sysconfigConfigureTargetSystem
	sysconfigConfigureTargetSystem = f
	return func() {
		sysconfigConfigureTargetSystem = old
	}
}
