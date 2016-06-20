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

// Package bind implements bind mounts that get mapped into the snap
//
// Snappy creates fstab like  configuration files that describe what
// directories from the system or from other snaps should get mapped
// into the snap.
//
// Each fstab like file looks like a regular fstab entry:
//   /src/dir /dst/dir none bind 0 0
//   /src/dir /dst/dir none bind,rw 0 0
// but only bind mounts are supported
package bind

import (
	"bytes"
	"fmt"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining bind files for snap-confine
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "bind"
}

// Setup creates bind mount profile files specific to a given snap.
func (b *Backend) Setup(snapInfo *snap.Info, devMode bool, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name(), interfaces.SecurityBindMount)
	if err != nil {
		return fmt.Errorf("cannot obtain bind security snippets for snap %q: %s", snapName, err)
	}
	// Get the files that this snap should have
	content, err := b.combineSnippets(snapInfo, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected bind configuration files for snap %q: %s", snapName, err)
	}
	glob := fmt.Sprintf("%s.bind", interfaces.SecurityTagGlob(snapName))
	dir := dirs.SnapBindMountPolicyDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for bind configuration files %q: %s", dir, err)
	}
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize bind configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// Remove removes bind configuration files of a given snap.
//
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	glob := fmt.Sprintf("%s.bind", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapBindMountPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize bind configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// combineSnippets combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) combineSnippets(snapInfo *snap.Info, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		appSnippets := snippets[appInfo.Name]
		if len(appSnippets) == 0 {
			continue
		}
		var buf bytes.Buffer
		for _, snippet := range appSnippets {
			buf.Write(snippet)
			buf.WriteRune('\n')
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		fname := fmt.Sprintf("%s.bind", appInfo.SecurityTag())
		content[fname] = &osutil.FileState{Content: buf.Bytes(), Mode: 0644}
	}
	return content, nil
}
