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

// Package kmod implements a backend which loads kernel modules on behalf of
// interfaces.
//
// Interfaces may request kernel modules to be loaded by providing snippets via
// their respective "*Snippet" methods for interfaces.SecurityKMod security
// system. The snippet should contain a newline-separated list of requested
// kernel modules. The KMod backend stores all the modules needed by given
// snap in /etc/modules-load.d/snap.<snapname>.conf file ensuring they are
// loaded when the system boots and also loads these modules via modprobe.
// If a snap is uninstalled or respective interface gets disconnected, the
// corresponding /etc/modules-load.d/ config file gets removed, however no
// kernel modules are unloaded. This is by design.
//
// Note: this mechanism should not be confused with kernel-module-interface;
// kmod only loads a well-defined list of modules provided by interface definition
// and doesn't grant any special permissions related to kernel modules to snaps,
// in contrast to kernel-module-interface.
package kmod

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

// Backend is responsible for maintaining kernel modules
type Backend struct {
	preseed bool
}

// Initialize does nothing.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	if opts != nil && opts.Preseed {
		b.preseed = true
	}
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return "kmod"
}

// setupModules creates a conf file with list of kernel modules required by
// given snap, writes it in /etc/modules-load.d/ directory and immediately
// loads the modules using /sbin/modprobe. The devMode is ignored.
func (b *Backend) setupModules(appSet *interfaces.SnapAppSet, spec *Specification) error {
	content, modules := deriveContent(spec, appSet)
	// synchronize the content with the filesystem
	globs := interfaces.SecurityTagGlobs(appSet.InstanceName())
	dir := dirs.SnapKModModulesDir
	mylog.Check(os.MkdirAll(dir, 0755))

	changed, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapKModModulesDir, globs, content))

	if len(changed) > 0 {
		b.loadModules(modules)
	}
	return nil
}

// setupModprobe creates a configuration file under /etc/modprobe.d/ according
// to the specification: this allows to either specify the load parameters for
// a module, or prevent it from being loaded.
// TODO: consider whether
// - a newly blocklisted module should get unloaded
// - a module whose option change should get reloaded
func (b *Backend) setupModprobe(appSet *interfaces.SnapAppSet, spec *Specification) error {
	dir := dirs.SnapKModModprobeDir
	mylog.Check(os.MkdirAll(dir, 0755))

	globs := interfaces.SecurityTagGlobs(appSet.InstanceName())
	dirContents := prepareModprobeDirContents(spec, appSet)
	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapKModModprobeDir, globs, dirContents))

	return nil
}

// Setup will make the kmod backend generate the needed system files (such as
// those under /etc/modules-load.d/ and /etc/modprobe.d/) and call the
// appropriate system commands so that the desired kernel module configuration
// will be applied.
// The devMode is ignored.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, confinement interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	snapName := appSet.InstanceName()
	// Get the snippets that apply to this snap
	spec := mylog.Check2(repo.SnapSpecification(b.Name(), appSet))
	mylog.Check(b.setupModprobe(appSet, spec.(*Specification)))
	mylog.Check(b.setupModules(appSet, spec.(*Specification)))

	return nil
}

// Remove removes modules config file specific to a given snap.
//
// This method should be called after removing a snap.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Remove(snapName string) error {
	globs := interfaces.SecurityTagGlobs(snapName)
	var errors []error
	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapKModModulesDir, globs, nil))

	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapKModModprobeDir, globs, nil))

	if len(errors) > 0 {
		return fmt.Errorf("cannot remove kernel modules config files: %v", errors)
	}

	return nil
}

func deriveContent(spec *Specification, appSet *interfaces.SnapAppSet) (map[string]osutil.FileState, []string) {
	if len(spec.modules) == 0 {
		return nil, nil
	}
	content := make(map[string]osutil.FileState)
	var modules []string
	for k := range spec.modules {
		modules = append(modules, k)
	}
	sort.Strings(modules)

	var buffer bytes.Buffer
	buffer.WriteString("# This file is automatically generated.\n")
	for _, module := range modules {
		buffer.WriteString(module)
		buffer.WriteRune('\n')
	}
	content[fmt.Sprintf("%s.conf", snap.SecurityTag(appSet.InstanceName()))] = &osutil.MemoryFileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
	return content, modules
}

func prepareModprobeDirContents(spec *Specification, appSet *interfaces.SnapAppSet) map[string]osutil.FileState {
	disallowedModules := spec.DisallowedModules()
	if len(disallowedModules) == 0 && len(spec.moduleOptions) == 0 {
		return nil
	}

	contents := "# Generated by snapd. Do not edit\n\n"
	// First, write down the list of disallowed modules
	for _, module := range disallowedModules {
		contents += fmt.Sprintf("blacklist %s\n", module)
	}
	// Then, write down the module options
	for module, options := range spec.moduleOptions {
		contents += fmt.Sprintf("options %s %s\n", module, options)
	}

	fileName := fmt.Sprintf("%s.conf", snap.SecurityTag(appSet.InstanceName()))
	return map[string]osutil.FileState{
		fileName: &osutil.MemoryFileState{
			Content: []byte(contents),
			Mode:    0644,
		},
	}
}

func (b *Backend) NewSpecification(*interfaces.SnapAppSet) interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of features supported by snapd for loading kernel modules.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-modprobe"}
}
