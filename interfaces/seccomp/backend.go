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

// Package seccomp implements integration between snappy and
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
	"os"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Backend is responsible for maintaining seccomp profiles for ubuntu-core-launcher.
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "seccomp"
}

// Setup creates seccomp profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, devMode bool, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name(), interfaces.SecuritySecComp)
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapName, err)
	}
	// Get the files that this snap should have
	content, err := b.combineSnippets(snapInfo, devMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapName, err)
	}
	glob := interfaces.SecurityTagGlob(snapName)
	dir := dirs.SnapSeccompDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for seccomp profiles %q: %s", dir, err)
	}
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, err)
	}
	return nil
}

// Remove removes seccomp profiles of a given snap.
func (b *Backend) Remove(snapName string) error {
	glob := interfaces.SecurityTagGlob(snapName)
	_, _, err := osutil.EnsureDirState(dirs.SnapSeccompDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, err)
	}
	return nil
}

// combineSnippets combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) combineSnippets(snapInfo *snap.Info, devMode bool, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		var buf bytes.Buffer
		if devMode {
			// NOTE: This is going to be understood by ubuntu-core-launcher
			buf.WriteString("@complain\n")
		}
		buf.Write(defaultTemplate)
		for _, snippet := range snippets[appInfo.Name] {
			buf.Write(snippet)
			buf.WriteRune('\n')
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		fname := appInfo.SecurityTag()
		content[fname] = &osutil.FileState{
			Content: buf.Bytes(),
			Mode:    0644,
		}
	}
	return content, nil
}
