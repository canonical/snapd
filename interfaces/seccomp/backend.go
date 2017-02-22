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
	"sort"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining seccomp profiles for ubuntu-core-launcher.
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecuritySecComp
}

// Setup creates seccomp profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain seccomp specification for snap %q: %s", snapName, err)
	}

	// Get the snippets that apply to this snap
	content, err := b.deriveContent(spec.(*Specification), opts, snapInfo)
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

// deriveContent combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) deriveContent(spec *Specification, opts interfaces.ConfinementOptions, snapInfo *snap.Info) (content map[string]*osutil.FileState, err error) {
	/*for securityTag, snippet := range spec.Snippets {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		addContent(securityTag, opts, snippet, content)
	}*/

	for _, hookInfo := range snapInfo.Hooks {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		snippet := spec.Snippets[hookInfo.SecurityTag()]
		addContent(hookInfo.SecurityTag(), opts, snippet, content)
	}
	for _, appInfo := range snapInfo.Apps {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		snippet := spec.Snippets[appInfo.SecurityTag()]
		addContent(appInfo.SecurityTag(), opts, snippet, content)
	}

	return content, nil
}

func addContent(securityTag string, opts interfaces.ConfinementOptions, snippetsForTag [][]byte, content map[string]*osutil.FileState) {
	var buffer bytes.Buffer
	if opts.Classic && !opts.JailMode {
		// NOTE: This is understood by snap-confine
		buffer.WriteString("@unrestricted\n")
	}
	if opts.DevMode && !opts.JailMode {
		// NOTE: This is understood by snap-confine
		buffer.WriteString("@complain\n")
	}

	buffer.Write(defaultTemplate)
	sort.Sort(byByteContent(snippetsForTag))
	for _, snippet := range snippetsForTag {
		buffer.Write(snippet)
		buffer.WriteRune('\n')
	}

	content[securityTag] = &osutil.FileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
}

func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}

type byByteContent [][]byte

func (x byByteContent) Len() int      { return len(x) }
func (x byByteContent) Swap(a, b int) { x[a], x[b] = x[b], x[a] }
func (x byByteContent) Less(a, b int) bool {
	return bytes.Compare(x[a], x[b]) < 0
}
