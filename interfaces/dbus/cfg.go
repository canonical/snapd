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

// Package dbus implements interaction between snappy and dbus.
//
// Snappy creates dbus configuration files that describe how various
// services on the system bus can communicate with other peers.
//
// Each configuration is an XML file containing <busconfig>...</busconfig>.
// Particular security snippets define whole <policy>...</policy> entires.
//
// NOTE: This interacts with systemd.
// TODO: Explain how this works (security).
package dbus

import (
	"bytes"
	"fmt"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Configurator is responsible for maintaining configuration files for DBus.
type Configurator struct{}

// ConfigureSnapSecurity creates and loads security artefacts specific to a
// given snap.
//
// NOTE: Developer mode is not supported
func (cfg *Configurator) ConfigureSnapSecurity(repo *interfaces.Repository, snapInfo *snap.Info, developerMode bool) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, cfg.SecuritySystem())
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	// Get the files that this snap should have
	dir, glob, content, err := cfg.DirStateForInstalledSnap(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected dbus files for snap %q: %s", snapInfo.Name, err)
	}
	// NOTE: we don't care about particular file changes
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	return err
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
		return fmt.Errorf("cannot synchronize dbus files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// Finalize does nothing at all.
func (cfg *Configurator) Finalize() error {
	return nil
}

// SecuritySystem returns the constant interfaces.SecurityDBus.
func (cfg *Configurator) SecuritySystem() interfaces.SecuritySystem {
	return interfaces.SecurityDBus
}

// DirStateForInstalledSnap returns input for EnsureDirState() describing DBus
// configuration files for a given snap.
func (cfg *Configurator) DirStateForInstalledSnap(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (dir, glob string, content map[string]*osutil.FileState, err error) {
	dir = Directory()
	glob = configGlob(snapInfo)
	for _, appInfo := range snapInfo.Apps {
		if len(snippets) > 0 {
			// FIXME: what about developer mode?
			s := make([][]byte, 0, len(snippets[appInfo.Name])+2)
			s = append(s, xmlHeader)
			s = append(s, snippets[appInfo.Name]...)
			s = append(s, xmlFooter)
			fileContent := bytes.Join(s, []byte("\n"))
			if content == nil {
				content = make(map[string]*osutil.FileState)
			}
			content[configFile(appInfo)] = &osutil.FileState{Content: fileContent, Mode: 0644}
		}
	}
	return dir, glob, content, nil
}

// DirStateForRemovedSnap returns input for EnsureDirState() for removing any
// DBus configuration files that used to belong to a removed snap.
func (cfg *Configurator) DirStateForRemovedSnap(snapInfo *snap.Info) (dir, glob string) {
	dir = Directory()
	glob = configGlob(snapInfo)
	return dir, glob
}

// configFile returns the name of the DBus configuration file for a specific app.
//
// The return value must end with .conf to be considered by DBus as a configuration file.
func configFile(appInfo *snap.AppInfo) string {
	return fmt.Sprintf("%s.conf", interfaces.SecurityTag(appInfo))
}

// configGlob returns a glob matching names of all the DBus configuration files
// for a specific snap.
//
// The return value must match return value from configFile.
func configGlob(snapInfo *snap.Info) string {
	return fmt.Sprintf("%s.conf", interfaces.SecurityGlob(snapInfo))
}

// Directory is the DBus configuration directory.
//
// This constant must be changed in lock-step with ubuntu-core-launcher.
func Directory() string {
	return dirs.SnapBusPolicyDir
}
