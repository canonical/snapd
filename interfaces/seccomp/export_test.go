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

import (
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
)

// MockTemplate replaces seccomp template.
//
// NOTE: The real seccomp template is long. For testing it is convenient for
// replace it with a shorter snippet.
func MockTemplate(fakeTemplate []byte) (restore func()) {
	orig := defaultTemplate
	origBarePrivDropSyscalls := barePrivDropSyscalls
	defaultTemplate = fakeTemplate
	barePrivDropSyscalls = ""
	return func() {
		defaultTemplate = orig
		barePrivDropSyscalls = origBarePrivDropSyscalls
	}
}

func MockKernelFeatures(f func() []string) (resture func()) {
	old := kernelFeatures
	kernelFeatures = f
	return func() {
		kernelFeatures = old
	}
}

func MockRequiresSocketcall(f func(string) bool) (restore func()) {
	old := requiresSocketcall
	requiresSocketcall = f
	return func() {
		requiresSocketcall = old
	}
}

func MockDpkgKernelArchitecture(f func() string) (restore func()) {
	old := dpkgKernelArchitecture
	dpkgKernelArchitecture = f
	return func() {
		dpkgKernelArchitecture = old
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

func MockSeccompCompilerLookup(f func(string) (string, error)) (restore func()) {
	old := seccompCompilerLookup
	seccompCompilerLookup = f
	return func() {
		seccompCompilerLookup = old
	}
}

func (b *Backend) VersionInfo() seccomp_compiler.VersionInfo {
	return b.versionInfo
}

var (
	RequiresSocketcall = requiresSocketcall

	GlobalProfileLE = globalProfileLE
	GlobalProfileBE = globalProfileBE

	ParallelCompile = parallelCompile
)
