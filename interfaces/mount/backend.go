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

// Package mount implements mounts that get mapped into the snap
//
// Snappy creates fstab like  configuration files that describe what
// directories from the system or from other snaps should get mapped
// into the snap.
//
// Each fstab like file looks like a regular fstab entry:
//   /src/dir /dst/dir none bind 0 0
//   /src/dir /dst/dir none bind,rw 0 0
// but only bind mounts are supported
package mount

import (
	"bytes"
	"fmt"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining mount files for snap-confine
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecurityMount
}

// Setup creates mount mount profile files specific to a given snap.
func (b *Backend) Setup(snapInfo *snap.Info, confinement interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name(), interfaces.SecurityMount)
	if err != nil {
		return fmt.Errorf("cannot obtain mount security snippets for snap %q: %s", snapName, err)
	}
	// Get the files that this snap should have
	content, err := b.combineSnippets(snapInfo, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected mount configuration files for snap %q: %s", snapName, err)
	}
	glob := fmt.Sprintf("%s.fstab", interfaces.SecurityTagGlob(snapName))
	dir := dirs.SnapMountPolicyDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for mount configuration files %q: %s", dir, err)
	}
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize mount configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// Remove removes mount configuration files of a given snap.
//
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	glob := fmt.Sprintf("%s.fstab", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapMountPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize mount configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// combineSnippets combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) combineSnippets(snapInfo *snap.Info, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		securityTag := appInfo.SecurityTag()
		appSnippets := snippets[securityTag]
		if len(appSnippets) == 0 {
			continue
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}

		addContent(securityTag, appSnippets, content)
	}

	for _, hookInfo := range snapInfo.Hooks {
		securityTag := hookInfo.SecurityTag()
		hookSnippets := snippets[securityTag]
		if len(hookSnippets) == 0 {
			continue
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}

		addContent(securityTag, hookSnippets, content)
	}
	return content, nil
}

func addContent(securityTag string, executableSnippets [][]byte, content map[string]*osutil.FileState) {
	var buffer bytes.Buffer
	for _, snippet := range executableSnippets {
		buffer.Write(snippet)
		buffer.WriteRune('\n')
	}

	content[fmt.Sprintf("%s.fstab", securityTag)] = &osutil.FileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
}
