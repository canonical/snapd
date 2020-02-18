// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"github.com/snapcore/snapd/seed"
)

var (
	Run                     = run
	SystemSnapFromSeed      = systemSnapFromSeed
	CheckTargetSnapdVersion = checkTargetSnapdVersion
)

func MockOsGetuid(f func() int) (restore func()) {
	oldOsGetuid := osGetuid
	osGetuid = f
	return func() { osGetuid = oldOsGetuid }
}

func MockSyscallChroot(f func(string) error) (restore func()) {
	oldSyscallChroot := syscallChroot
	syscallChroot = f
	return func() { syscallChroot = oldSyscallChroot }
}

func MockSnapdMountPath(path string) (restore func()) {
	oldMountPath := snapdMountPath
	snapdMountPath = path
	return func() { snapdMountPath = oldMountPath }
}

func MockSystemSnapFromSeed(f func(rootDir string) (string, error)) (restore func()) {
	oldSystemSnapFromSeed := systemSnapFromSeed
	systemSnapFromSeed = f
	return func() { systemSnapFromSeed = oldSystemSnapFromSeed }
}

func MockSeedOpen(f func(rootDir, label string) (seed.Seed, error)) (restore func()) {
	oldSeedOpen := seedOpen
	seedOpen = f
	return func() {
		seedOpen = oldSeedOpen
	}
}
