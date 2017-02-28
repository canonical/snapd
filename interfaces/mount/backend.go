// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	// Record all changes to the mount system for this snap.
	snapName := snapInfo.Name()
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain mount security snippets for snap %q: %s", snapName, err)
	}
	content := deriveContent(spec.(*Specification), snapInfo)
	// synchronize the content with the filesystem
	glob := fmt.Sprintf("snap.%s.*fstab", snapName)
	dir := dirs.SnapMountPolicyDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for mount configuration files %q: %s", dir, err)
	}
	if _, _, err := osutil.EnsureDirState(dir, glob, content); err != nil {
		return fmt.Errorf("cannot synchronize mount configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// Remove removes mount configuration files of a given snap.
//
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	glob := fmt.Sprintf("snap.%s.*fstab", snapName)
	_, _, err := osutil.EnsureDirState(dirs.SnapMountPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize mount configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// deriveContent computes .fstab tables based on requests made to the specification.
func deriveContent(spec *Specification, snapInfo *snap.Info) map[string]*osutil.FileState {
	// No entries? Nothing to do!
	if len(spec.mountEntries) == 0 {
		return nil
	}
	// Compute the contents of the fstab file. It should contain all the mount
	// rules collected by the backend controller.
	var buffer bytes.Buffer
	for _, entry := range spec.mountEntries {
		fmt.Fprintf(&buffer, "%s\n", entry)
	}
	fstate := &osutil.FileState{Content: buffer.Bytes(), Mode: 0644}
	content := make(map[string]*osutil.FileState)
	// Add the new per-snap fstab file. This file will be read by snap-confine.
	content[fmt.Sprintf("snap.%s.fstab", snapInfo.Name())] = fstate
	// Add legacy per-app/per-hook fstab files. Those are identical but
	// snap-confine doesn't yet load it from a per-snap location. This can be
	// safely removed once snap-confine is updated.
	for _, appInfo := range snapInfo.Apps {
		content[fmt.Sprintf("%s.fstab", appInfo.SecurityTag())] = fstate
	}
	for _, hookInfo := range snapInfo.Hooks {
		content[fmt.Sprintf("%s.fstab", hookInfo.SecurityTag())] = fstate
	}
	return content
}

func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}
