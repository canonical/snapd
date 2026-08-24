// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

// Package export is a backend that copies files declared by interfaces out of
// a snap (or its components) into a staging tree under
// /var/lib/snapd/export/system/<interface-name>/, for later consumption by
// snap-confine. Unlike the configfiles and symlinks backends, which modify
// the classic rootfs, this backend targets a snapd-owned location that is
// meaningful on both classic and Ubuntu Core systems.
package export

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/timings"
)

// Backend is responsible for maintaining the snap export directory.
type Backend struct{}

var _ = interfaces.SecurityBackend(&Backend{})

// Initialize does nothing for this backend.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecurityExport
}

func (b *Backend) Prepare(_ *interfaces.SnapAppSet) error {
	// No preparation required.
	return nil
}

// Setup will make the export backend generate the files exported by
// connected interfaces.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository, tm timings.Measurer) error {
	// Collect the set of interfaces that use the export backend at all,
	// regardless of whether they currently have any connections. This is
	// what lets a fully disconnected interface's export tree be garbage
	// collected down to nothing, mirroring how the symlinks backend
	// collects symlinkDirs from interfaces.SymlinksUser.
	exportIfaces := map[string]bool{}
	for _, iface := range repo.AllInterfaces() {
		if _, ok := iface.(ConnectedPlugCallback); ok {
			exportIfaces[iface.Name()] = true
		}
	}

	snapName := appSet.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), appSet, opts)
	if err != nil {
		return fmt.Errorf("cannot obtain export specification for snap %q: %s",
			snapName, err)
	}

	return b.ensureExports(spec.(*Specification), exportIfaces)
}

// Remove removes exported files specific to a given snap.
// This method should be called after removing a snap.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Remove(snapName string) error {
	// If called for the system (snapd) snap, that is possible only in a
	// classic scenario when all other snaps in the system must have been
	// removed already to allow the removal of the snapd snap. In that
	// case, the exported files will have already been removed by a Setup
	// call, so we do not need to do anything here.

	// TODO but this needs to be revisited for when we start supporting
	// export plugs in snaps.
	return nil
}

// NewSpecification returns a new specification associated with this backend.
func (b *Backend) NewSpecification(*interfaces.SnapAppSet,
	interfaces.ConfinementOptions) interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of features supported by snapd for export.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-export"}
}
