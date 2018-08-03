// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package seccomp

// MockTemplate replaces seccomp template.
//
// NOTE: The real seccomp template is long. For testing it is convenient for
// replace it with a shorter snippet.
func MockTemplate(fakeTemplate []byte) (restore func()) {
	orig := defaultTemplate
	defaultTemplate = fakeTemplate
	return func() { defaultTemplate = orig }
}

func MockOsReadlink(f func(string) (string, error)) (restore func()) {
	realOsReadlink := osReadlink
	osReadlink = f
	return func() {
		osReadlink = realOsReadlink
	}
}

func MockKernelFeatures(f func() []string) (resture func()) {
	old := kernelFeatures
	kernelFeatures = f
	return func() {
		kernelFeatures = old
	}
}

func MockRequiresSocketcall(f func() bool) (restore func()) {
	old := requiresSocketcall
	requiresSocketcall = f
	return func() {
		requiresSocketcall = old
	}
}

func MockUbuntuKernelArchitecture(f func() string) (restore func()) {
	old := ubuntuKernelArchitecture
	ubuntuKernelArchitecture = f
	return func() {
		ubuntuKernelArchitecture = old
	}
}

func MockKernelVersion(f func() string) (restore func()) {
	old := kernelVersion
	kernelVersion = f
	return func() {
		kernelVersion = old
	}
}

func MockReleaseInfoId(s string) (restore func()) {
	old := releaseInfoId
	releaseInfoId = s
	return func() {
		releaseInfoId = old
	}
}

func MockReleaseInfoVersionId(s string) (restore func()) {
	old := releaseInfoVersionId
	releaseInfoVersionId = s
	return func() {
		releaseInfoVersionId = old
	}
}
