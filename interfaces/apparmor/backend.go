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
// present in the system.  Upon each execution of ubuntu-core-launcher
// application process is launched under the profile. Prior to that the profile
// must be parsed, compiled and loaded into the kernel using the support tool
// "apparmor_parser".
//
// Each apparmor profile contains a simple <header><content><footer> structure.
// The header specifies the profile name that the launcher will use to launch a
// process under this profile.  Snappy uses "abstract identifiers" as profile
// names.
//
// The actual profiles are stored in /var/lib/snappy/apparmor/profiles.
//
// NOTE: A systemd job (apparmor.service) loads all snappy-specific apparmor
// profiles into the kernel during the boot process.
package apparmor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Backend is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Backend struct {
	// legacyTemplate exists to support old-security which goes
	// beyond what is possible with pure security snippets.
	//
	// If non-empty then it overrides the built-in template.
	legacyTemplate []byte
}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "apparmor"
}

// UseLegacyTemplate switches from default apparmor template to a custom
// template. This also implies that a fixed set of apparmor variables will be
// injected into this template. The set is compatible with Ubuntu core 15.04.
func (b *Backend) UseLegacyTemplate(template []byte) {
	b.legacyTemplate = template
}

// Setup creates and loads apparmor profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, developerMode bool, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapName, interfaces.SecurityAppArmor)
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapName, err)
	}
	// Get the files that this snap should have
	content, err := b.combineSnippets(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapName, err)
	}
	glob := interfaces.SecurityTagGlob(snapInfo.Name())
	dir := dirs.SnapAppArmorDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for apparmor profiles %q: %s", dir, err)
	}
	changed, removed, errEnsure := osutil.EnsureDirState(dir, glob, content)
	errReload := reloadProfiles(changed)
	errUnload := unloadProfiles(removed)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}
	if errReload != nil {
		return errReload
	}
	return errUnload
}

// Remove removes and unloads apparmor profiles of a given snap.
func (b *Backend) Remove(snapName string) error {
	glob := interfaces.SecurityTagGlob(snapName)
	_, removed, errEnsure := osutil.EnsureDirState(dirs.SnapAppArmorDir, glob, nil)
	errUnload := unloadProfiles(removed)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}
	return errUnload
}

var (
	templatePattern          = regexp.MustCompile("(###[A-Z]+###)")
	placeholderVar           = []byte("###VAR###")
	placeholderSnippets      = []byte("###SNIPPETS###")
	placeholderProfileAttach = []byte("###PROFILEATTACH###")
	attachPattern            = regexp.MustCompile(`\(attach_disconnected\)`)
	attachComplain           = []byte("(attach_disconnected,complain)")
)

// combineSnippets combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState. The
// backend delegates writing those files to higher layers.
func (b *Backend) combineSnippets(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		policy := b.legacyTemplate
		if policy == nil {
			policy = defaultTemplate
		}
		if developerMode {
			policy = attachPattern.ReplaceAll(policy, attachComplain)
		}
		policy = templatePattern.ReplaceAllFunc(policy, func(placeholder []byte) []byte {
			switch {
			case bytes.Equal(placeholder, placeholderVar):
				// TODO: use modern variables when default template is compatible
				// with them and the custom template is not used.
				return legacyVariables(appInfo)
			case bytes.Equal(placeholder, placeholderProfileAttach):
				return []byte(fmt.Sprintf("profile \"%s\"", appInfo.SecurityTag()))
			case bytes.Equal(placeholder, placeholderSnippets):
				return bytes.Join(snippets[appInfo.Name], []byte("\n"))
			}
			return nil
		})
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		fname := appInfo.SecurityTag()
		content[fname] = &osutil.FileState{
			Content: policy,
			Mode:    0644,
		}
	}
	return content, nil
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
		if err := UnloadProfile(profile); err != nil {
			return fmt.Errorf("cannot unload apparmor profile %q: %s", profile, err)
		}
	}
	return nil
}
