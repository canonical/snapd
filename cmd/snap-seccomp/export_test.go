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

var (
	Compile           = compile
	SeccompResolver   = seccompResolver
	VersionInfo       = versionInfo
	GoSeccompFeatures = goSeccompFeatures
)

func MockArchUbuntuArchitecture(f func() string) (restore func()) {
	realArchUbuntuArchitecture := archDpkgArchitecture
	archDpkgArchitecture = f
	return func() {
		archDpkgArchitecture = realArchUbuntuArchitecture
	}
}

func MockArchUbuntuKernelArchitecture(f func() string) (restore func()) {
	realArchUbuntuKernelArchitecture := archDpkgKernelArchitecture
	archDpkgKernelArchitecture = f
	return func() {
		archDpkgKernelArchitecture = realArchUbuntuKernelArchitecture
	}
}

func MockErrnoOnDenial(i int16) (retore func()) {
	origErrnoOnDenial := errnoOnDenial
	errnoOnDenial = i
	return func() {
		errnoOnDenial = origErrnoOnDenial
	}
}

func MockSeccompSyscalls(syscalls []string) (resture func()) {
	old := seccompSyscalls
	seccompSyscalls = syscalls
	return func() {
		seccompSyscalls = old
	}
}
