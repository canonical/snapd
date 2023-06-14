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

func MockArchDpkgArchitecture(f func() string) (restore func()) {
	realArchDpkgArchitecture := archDpkgArchitecture
	archDpkgArchitecture = f
	return func() {
		archDpkgArchitecture = realArchDpkgArchitecture
	}
}

func MockArchDpkgKernelArchitecture(f func() string) (restore func()) {
	realArchDpkgKernelArchitecture := archDpkgKernelArchitecture
	archDpkgKernelArchitecture = f
	return func() {
		archDpkgKernelArchitecture = realArchDpkgKernelArchitecture
	}
}

func MockErrnoOnImplicitDenial(i int16) (retore func()) {
	origErrnoOnImplicitDenial := errnoOnImplicitDenial
	errnoOnImplicitDenial = i
	return func() {
		errnoOnImplicitDenial = origErrnoOnImplicitDenial
	}
}

func MockErrnoOnExplicitDenial(i int16) (retore func()) {
	origErrnoOnExplicitDenial := errnoOnExplicitDenial
	errnoOnExplicitDenial = i
	return func() {
		errnoOnExplicitDenial = origErrnoOnExplicitDenial
	}
}

func MockSeccompSyscalls(syscalls []string) (resture func()) {
	old := seccompSyscalls
	seccompSyscalls = syscalls
	return func() {
		seccompSyscalls = old
	}
}
