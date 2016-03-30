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
	// Parsed chunks of the template
	imports, body []byte
	// legacy indicates that 15.04 compatible apparmor variables are required.
	legacy bool
}

// UseLegacyTemplate uses the given apparmor template.
func (b *Backend) UseLegacyTemplate(template []byte) error {
	imports, body, err := parseTemplate(template)
	if err != nil {
		return err
	}
	b.imports = imports
	b.body = body
	b.legacy = true
	return nil
}

// Configure creates and loads apparmor security profiles specific to a given
// snap. The snap can be in developer mode to make security violations
// non-fatal to the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Configure(snapInfo *snap.Info, developerMode bool, repo *interfaces.Repository) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, interfaces.SecurityAppArmor)
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	content := b.combineSnippets(snapInfo, developerMode, snippets)
	glob := interfaces.SecurityTagGlob(snapInfo)
	changed, removed, err := osutil.EnsureDirState(dirs.SnapAppArmorDir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapInfo.Name, err)
	}
	err = reloadProfiles(changed)
	if err != nil {
		return err
	}
	err = unloadProfiles(removed)
	if err != nil {
		return err
	}
	return nil
}

// Deconfigure removes security artefacts of a given snap.
func (b *Backend) Deconfigure(snapInfo *snap.Info) error {
	glob := interfaces.SecurityTagGlob(snapInfo)
	_, removed, err := osutil.EnsureDirState(dirs.SnapAppArmorDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapInfo.Name, err)
	}
	err = unloadProfiles(removed)
	if err != nil {
		return err
	}
	return nil
}

// combineSnippets combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState. The
// backend delegates writing those files to higher layers.
func (b *Backend) combineSnippets(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (content map[string]*osutil.FileState) {
	b.parseDefaultTemplateIfNeeded()
	for _, appInfo := range snapInfo.Apps {
		var buf bytes.Buffer
		buf.Write(b.imports)
		if b.legacy {
			writeLegacyVariables(&buf, appInfo)
		} else {
			writeModernVariables(&buf, appInfo)
		}
		writeProfileAttach(&buf, appInfo, developerMode)
		buf.Write(b.body)
		for _, snippet := range snippets[appInfo.Name] {
			buf.Write(snippet)
			buf.WriteRune('\n')
		}
		buf.WriteString("}\n")
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		content[interfaces.SecurityTag(appInfo)] = &osutil.FileState{Content: buf.Bytes(), Mode: 0644}
	}
	return content
}

func (b *Backend) parseDefaultTemplateIfNeeded() {
	if b.imports == nil || b.body == nil {
		imports, body, err := parseTemplate(defaultTemplate)
		if err != nil {
			panic(err)
		}
		b.imports = imports
		b.body = body
		// TODO: switch to non-legacy mode when default template
		// uses only modern variables.
		b.legacy = true
	}
}

func reloadProfiles(profiles []string) error {
	for _, profile := range profiles {
		fname := filepath.Join(dirs.SnapAppArmorDir, profile)
		err := LoadProfile(fname)
		if err != nil {
			return fmt.Errorf("cannot load apparmor profile %q: %s", profile, err)
		}
	}
	return nil
}

func unloadProfiles(profiles []string) error {
	for _, profile := range profiles {
		err := UnloadProfile(profile)
		if err != nil {
			return fmt.Errorf("cannot unload apparmor profile %q: %s", profile, err)
		}
	}
	return nil
}
