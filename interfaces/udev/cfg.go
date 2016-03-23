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

// Package udev implements integration between snappy, udev and
// ubuntu-core-laucher around tagging character and block devices so that they
// can be accessed by applications.
//
// TODO: Document this better
package udev

import (
	"bytes"
	"fmt"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Configurator is responsible for maintaining configuration files for udev
type Configurator struct {
	needsReload bool
}

// ConfigureSnapSecurity creates security artefacts specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method only deals with files. If any of the rules are changed udev
// doesn't know about it until we notify it. This is deferred to a call to
// Finalize
//
// NOTE: Developer mode is not implemented.
func (cfg *Configurator) ConfigureSnapSecurity(repo *interfaces.Repository, snapInfo *snap.Info, developerMode bool) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, cfg.SecuritySystem())
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	// Get the files that this snap should have
	dir, glob, content, err := cfg.DirStateForInstalledSnap(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected udev files for snap %q: %s", snapInfo.Name, err)
	}
	changed, removed, err := osutil.EnsureDirState(dir, glob, content)
	if len(changed) > 0 || len(removed) > 0 {
		cfg.needsReload = true
	}
	if err != nil {
		return fmt.Errorf("cannot synchronize udev files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// DeconfigureSnapSecurity removes security artefacts of a given snap.
//
// This method should be called after removing a snap.
//
// This method only deals with the files. If any of the rules are changed udev
// doesn't know about it until we notify it. This is deferred to a call to
// Finalize
func (cfg *Configurator) DeconfigureSnapSecurity(snapInfo *snap.Info) error {
	dir, glob := cfg.DirStateForRemovedSnap(snapInfo)
	changed, removed, err := osutil.EnsureDirState(dir, glob, nil)
	if len(changed) > 0 {
		panic(fmt.Sprintf("removed snaps cannot have security files but we got %s", changed))
	}
	if len(removed) > 0 {
		cfg.needsReload = true
	}
	if err != nil {
		return fmt.Errorf("cannot synchronize udev files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// Finalize does nothing at all.
func (cfg *Configurator) Finalize() error {
	if cfg.needsReload {
		cfg.needsReload = false
		return ReloadRules()
	}
	return nil
}

// SecuritySystem returns the constant interfaces.SecurityUDev.
func (cfg *Configurator) SecuritySystem() interfaces.SecuritySystem {
	return interfaces.SecurityUDev
}

// DirStateForInstalledSnap returns input for EnsureDirState() describing udev configuration files for a given snap.
func (cfg *Configurator) DirStateForInstalledSnap(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (dir, glob string, content map[string]*osutil.FileState, err error) {
	dir = Directory()
	glob = ruleGlob(snapInfo)
	for _, appInfo := range snapInfo.Apps {
		// FIXME: what about developer mode?
		if len(snippets) == 0 {
			continue
		}
		fileContent := bytes.Join(snippets[appInfo.Name], []byte("\n"))
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		content[ruleFile(appInfo)] = &osutil.FileState{
			Content: fileContent,
			Mode:    0644,
		}
	}
	return dir, glob, content, nil
}

// DirStateForRemovedSnap returns input for EnsureDirState() for removing any
// udev configuration files that used to belong to a removed snap.
func (cfg *Configurator) DirStateForRemovedSnap(snapInfo *snap.Info) (dir, glob string) {
	dir = Directory()
	glob = ruleGlob(snapInfo)
	return dir, glob
}

// ruleFile returns the name of the udev configuration file for a specific app.
//
// The return value must end in ".rules" to be considered by udev as a rule file.
func ruleFile(appInfo *snap.AppInfo) string {
	return fmt.Sprintf("70-%s.rules", interfaces.SecurityTag(appInfo))
}

// ruleGlob returns a glob matching names of all the udev configuration files for a specific snap.
//
// The return value must match return value from ruleFile.
func ruleGlob(snapInfo *snap.Info) string {
	return fmt.Sprintf("70-%s.rules", interfaces.SecurityGlob(snapInfo))
}

// Env returns udev environment variables for an udev rule.
//
// Interface implementations must use this function to compute a compatible
// security snippet.
func Env(appInfo *snap.AppInfo) map[string]string {
	return map[string]string{"SNAPPY_APP": interfaces.SecurityTag(appInfo)}
}

// Tag is the udev tag used by both snappy and ubuntu-core-launcher.
//
// Interface implementations must use this constant to compute a compatible
// security snippet.
const Tag = "snappy-assign"

// Directory is the udev configuration directory.
//
// All of the configuration files are stored in this directory.
func Directory() string {
	return dirs.SnapUdevRulesDir
}
