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
type Backend struct {
}

// BackendCtrl assists in collecting mount entries associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the mount backend during the interface
// setup process.
type BackendCtrl struct {
	mountEntries []Entry
}

// AddMountEntry adds a new mount entry.
func (b *BackendCtrl) AddMountEntry(e Entry) {
	b.mountEntries = append(b.mountEntries, e)
}

// MountAware describes the APIs required to interact with the mount backend.
// Each of the four methods there replaces one of the older snippet based
// methods. Instead of returning a snippet those methods should call APIs on
// the backend instance (e.g. the AddMountEntry method) to express their
// intents.
type MountAware interface {
	// ConnectedPlugMounts registers mount entries desired when a given plug
	// and slot are connected. The entries will be effective in the snap
	// containing the plug.
	ConnectedPlugMounts(b *BackendCtrl, plug *interfaces.Plug, slot *interfaces.Slot) error

	// ConnectedSlotMounts registers mount entries desired when a given plug
	// and slot are connected. The entries will be effective in the snap
	// containing the slot.
	ConnectedSlotMounts(b *BackendCtrl, plug *interfaces.Plug, slot *interfaces.Slot) error

	// PermanentPlugMounts registers mount entries desired whenever a given
	// plug exists.
	PermanentPlugMounts(b *BackendCtrl, plug *interfaces.Plug) error

	// PermanentSlotMounts registers mount entries desired whenever a given
	// slot exists.
	PermanentSlotMounts(b *BackendCtrl, slot *interfaces.Slot) error
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecurityMount
}

// Setup creates mount mount profile files specific to a given snap.
func (b *Backend) Setup(snapInfo *snap.Info, confinement interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// TODO: replace this with a pass over all the interfaces, each one that
	// implements MountAware interface gets interrogated. The rest is similar
	// with the exception that merging the collected data is easier.
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

func (b *Backend) NewSpecification() interfaces.Specification {
	panic(fmt.Errorf("%s is not using specifications yet", b.Name()))
}
