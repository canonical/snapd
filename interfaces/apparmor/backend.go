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

// Package apparmor implements integration between snappy and
// ubuntu-core-launcher around apparmor.
//
// Snappy creates apparmor profiles for each application (for each snap)
// present in the system.  Upon each execution of ubuntu-core-launcher, the
// profile is attached to the running process. Prior to that the profile must
// be parsed, compiled and loaded into the kernel using the support tool
// "apparmor_parser".
//
// Each apparmor profile contains a simple <header><content><footer> structure.
// The header specified an identifier that is relevant to the kernel. The
// identifier can be either the full path of the executable or an abstract
// identifier not related to the executable name.
//
// The actual profiles are stored in /var/lib/snappy/apparmor/profiles.
// This directory is also hard-coded in ubuntu-core-launcher.
//
// NOTE: A systemd job (TODO: specify which) loads all snappy-specific apparmor
// profiles into the kernel during the boot process.
package apparmor

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Backend is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Backend struct {
	// CustomTemplate exists to support old-security which goes
	// beyond what is possible with pure security snippets.
	//
	// If non-empty then it overrides the built-in template.
	CustomTemplate string
}

// Configure creates and loads security artefacts specific to a given snap. The
// snap can be in developer mode to make security violations non-fatal to the
// offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Configure(snapInfo *snap.Info, developerMode bool, repo *interfaces.Repository) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, interfaces.SecurityAppArmor)
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	// Get the files that this snap should have
	content, err := b.CombineSnippets(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapInfo.Name, err)
	}
	changed, removed, err := osutil.EnsureDirState(b.Directory(), b.FileGlob(snapInfo), content)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapInfo.Name, err)
	}
	err = b.ObserveChanges(changed, removed)
	if err != nil {
		return err
	}
	return nil
}

// Deconfigure removes security artefacts of a given snap.
func (b *Backend) Deconfigure(snapInfo *snap.Info) error {
	_, removed, err := osutil.EnsureDirState(b.Directory(), b.FileGlob(snapInfo), nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapInfo.Name, err)
	}
	err = b.ObserveChanges(nil, removed)
	if err != nil {
		return err
	}
	return nil
}

// CombineSnippets combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState. The
// backend delegates writing those files to higher layers.
func (b *Backend) CombineSnippets(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		s := make([][]byte, 0, len(snippets[appInfo.Name])+2)
		s = append(s, b.aaHeader(appInfo, developerMode))
		s = append(s, snippets[appInfo.Name]...)
		s = append(s, []byte("}\n"))
		fileContent := bytes.Join(s, []byte("\n"))
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		content[b.FileName(appInfo)] = &osutil.FileState{
			Content: fileContent,
			Mode:    0644,
		}
	}
	return content, nil
}

// ObserveChanges informs the backend about changes made to the set of managed files.
func (b *Backend) ObserveChanges(changed, removed []string) error {
	// Reload changed profiles
	for _, baseName := range changed {
		fname := filepath.Join(b.Directory(), baseName)
		err := LoadProfile(fname)
		if err != nil {
			return fmt.Errorf("cannot load apparmor profile %q: %s", baseName, err)
		}
	}
	// Unload removed profiles
	for _, baseName := range removed {
		err := UnloadProfile(baseName)
		if err != nil {
			return fmt.Errorf("cannot unload apparmor profile %q: %s", baseName, err)
		}
	}
	return nil
}

// Directory returns the apparmor configuration directory.
//
// The return value must be changed in lock-step with the systemd job that
// loads profiles on boot.
func (b *Backend) Directory() string {
	return dirs.SnapAppArmorDir
}

// FileName returns the name of security file associated with a given application.
func (b *Backend) FileName(appInfo *snap.AppInfo) string {
	return interfaces.SecurityTag(appInfo)
}

// FileGlob returns the pattern describing all security files associated with a given snap.
func (b *Backend) FileGlob(snapInfo *snap.Info) string {
	return interfaces.SecurityTagGlob(snapInfo)
}
