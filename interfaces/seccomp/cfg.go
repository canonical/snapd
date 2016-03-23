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

// Package seccomp implements interaction between snappy and
// ubuntu-core-launcher around seccomp.
//
// Snappy creates so-called seccomp profiles for each application (for each
// snap) present in the system.  Upon each execution of ubuntu-core-launcher,
// the profile is read and "compiled" to an eBPF program and injected into the
// kernel for the duration of the execution of the process.
//
// There is no binary cache for seccomp, each time the launcher starts an
// application the profile is parsed and re-compiled.
//
// The actual profiles are stored in /var/lib/snappy/seccomp/profiles.
// This directory is hard-coded in ubuntu-core-launcher.
package seccomp

import (
	"bytes"
	"fmt"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Configurator is responsible for maintaining seccomp profiles for ubuntu-core-launcher.
type Configurator struct {
}

// ConfigureSnapSecurity creates and loads security artefacts specific to a
// given snap. The snap can be in developer mode to make security violations
// non-fatal to the offending application process.
func (cfg *Configurator) ConfigureSnapSecurity(repo *interfaces.Repository, snapInfo *snap.Info, developerMode bool) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, cfg.SecuritySystem())
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	// Get the files that this snap should have
	dir, glob, content, err := cfg.DirStateForInstalledSnap(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected seccomp files for snap %q: %s", snapInfo.Name, err)
	}
	// NOTE: we don't care about particular file changes
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize seccomp profiles for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// DeconfigureSnapSecurity removes security artefacts of a given snap.
//
// This method should be called after removing a snap.
func (cfg *Configurator) DeconfigureSnapSecurity(snapInfo *snap.Info) error {
	dir, glob := cfg.DirStateForRemovedSnap(snapInfo)
	changed, _, err := osutil.EnsureDirState(dir, glob, nil)
	if len(changed) > 0 {
		panic(fmt.Sprintf("removed snaps cannot have security files but we got %s", changed))
	}
	if err != nil {
		return fmt.Errorf("cannot synchronize seccomp files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// Finalize does nothing at all.
func (cfg *Configurator) Finalize() error {
	return nil
}

// SecuritySystem returns the constant interfaces.SecuritySecComp.
func (cfg *Configurator) SecuritySystem() interfaces.SecuritySystem {
	return interfaces.SecuritySecComp
}

// DirStateForInstalledSnap returns input for EnsureDirState() describing
// seccomp profiles for the given snap.
func (cfg *Configurator) DirStateForInstalledSnap(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (dir, glob string, content map[string]*osutil.FileState, err error) {
	dir = Directory()
	glob = profileGlob(snapInfo)
	for _, appInfo := range snapInfo.Apps {
		var fileContent []byte
		if developerMode {
			// NOTE: This is understood by ubuntu-core-launcher
			fileContent = []byte("@unrestricted\n")
		} else {
			// TODO: process snippets for nicer results:
			// - discard content including and after '#' (comments)
			// - trim spaces
			// - discard empty lines
			// - sort output (preferably with /deny .+/ before everything else).
			// - remove duplicates
			s := make([][]byte, 0, len(snippets[appInfo.Name])+1)
			// Inject the default template as implicit first snippet
			s = append(s, defaultTemplate)
			s = append(s, snippets[appInfo.Name]...)
			fileContent = bytes.Join(s, []byte("\n"))
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		content[ProfileFile(appInfo)] = &osutil.FileState{
			Content: fileContent,
			Mode:    0644,
		}
	}
	return dir, glob, content, nil
}

// DirStateForRemovedSnap returns input for EnsureDirState() for removing any
// seccomp files that used to belong to a removed snap.
func (cfg *Configurator) DirStateForRemovedSnap(snapInfo *snap.Info) (dir, glob string) {
	dir = Directory()
	glob = profileGlob(snapInfo)
	return dir, glob
}

// ProfileFile returns the name of the seccomp profile file for a specific app.
//
// The return value must be changed in lock-step with ubuntu-core-launcher.
// The return value must be used as an argument to ubuntu-core-launcher.
func ProfileFile(appInfo *snap.AppInfo) string {
	return interfaces.SecurityTag(appInfo)
}

// profileGlob returns a glob matching names of all the seccomp profile
// files for a specific snap.
//
// The return value must match return value from ProfileFile.
func profileGlob(snapInfo *snap.Info) string {
	return interfaces.SecurityGlob(snapInfo)
}

// Directory is the seccomp configuration directory.
//
// This constant must be changed in lock-step with ubuntu-core-launcher.
func Directory() string {
	return dirs.SnapSeccompDir
}
