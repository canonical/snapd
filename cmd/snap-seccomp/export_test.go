// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	Compile         = compile
	SeccompResolver = seccompResolver
)

func MockArchUbuntuArchitecture(f func() string) (restore func()) {
	realArchUbuntuArchitecture := archUbuntuArchitecture
	archUbuntuArchitecture = f
	return func() {
		archUbuntuArchitecture = realArchUbuntuArchitecture
	}
}

func MockArchUbuntuKernelArchitecture(f func() string) (restore func()) {
	realArchUbuntuKernelArchitecture := archUbuntuKernelArchitecture
	archUbuntuKernelArchitecture = f
	return func() {
		archUbuntuKernelArchitecture = realArchUbuntuKernelArchitecture
	}
}

func MockErrnoOnDenial(i int16) (retore func()) {
	origErrnoOnDenial := errnoOnDenial
	errnoOnDenial = i
	return func() {
		errnoOnDenial = origErrnoOnDenial
	}
}
