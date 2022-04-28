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

package apparmor

import (
	"os"

	"github.com/snapcore/snapd/snap"
)

var (
	NsProfile                       = nsProfile
	ProfileGlobs                    = profileGlobs
	SnapConfineFromSnapProfile      = snapConfineFromSnapProfile
	DefaultCoreRuntimeTemplateRules = defaultCoreRuntimeTemplateRules
	DefaultOtherBaseTemplateRules   = defaultOtherBaseTemplateRules
)

// MockIsRootWritableOverlay mocks the real implementation of osutil.IsRootWritableOverlay
func MockIsRootWritableOverlay(new func() (string, error)) (restore func()) {
	old := isRootWritableOverlay
	isRootWritableOverlay = new
	return func() {
		isRootWritableOverlay = old
	}
}

// MockProcSelfExe mocks the location of /proc/self/exe read by setupSnapConfineGeneratedPolicy.
func MockProcSelfExe(symlink string) (restore func()) {
	old := procSelfExe
	procSelfExe = symlink
	return func() {
		os.Remove(procSelfExe)
		procSelfExe = old
	}
}

// MockTemplate replaces apprmor template.
//
// NOTE: The real apparmor template is long. For testing it is convenient for
// replace it with a shorter snippet.
func MockTemplate(fakeTemplate string) (restore func()) {
	orig := defaultCoreRuntimeTemplate
	defaultCoreRuntimeTemplate = fakeTemplate
	return func() { defaultCoreRuntimeTemplate = orig }
}

// MockClassicTemplate replaces the classic apprmor template.
func MockClassicTemplate(fakeTemplate string) (restore func()) {
	orig := classicTemplate
	classicTemplate = fakeTemplate
	return func() { classicTemplate = orig }
}

// SetSpecScope sets the scope of a given specification
func SetSpecScope(spec *Specification, securityTags []string) (restore func()) {
	return spec.setScope(securityTags)
}

func MockKernelFeatures(f func() ([]string, error)) (resture func()) {
	old := kernelFeatures
	kernelFeatures = f
	return func() {
		kernelFeatures = old
	}
}

func MockParserFeatures(f func() ([]string, error)) (resture func()) {
	old := parserFeatures
	parserFeatures = f
	return func() {
		parserFeatures = old
	}
}

func (b *Backend) SetupSnapConfineReexec(info *snap.Info) error {
	return b.setupSnapConfineReexec(info)
}

func (s *Specification) SnippetsForTag(tag string) []string {
	return s.snippetsForTag(tag)
}
