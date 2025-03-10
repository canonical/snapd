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

// configfiles is a backend that ensures that configuration files required by
// interfaces are present in the system. Currently it works only on classic and
// modifies the classic rootfs.
package configfiles

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/timings"
)

// Backend is responsible for maintaining configfiles cache.
type Backend struct{}

var _ = interfaces.SecurityBackend(&Backend{})

// Initialize does nothing for this backend.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return "configfiles"
}

// Setup will make the configfiles backend generate the specified
// configuration files.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	cfgPatterns := []string{}
	for _, iface := range repo.AllInterfaces() {
		if cfgIface, ok := iface.(interfaces.ConfigfilesUser); ok {
			cfgPatterns = append(cfgPatterns, cfgIface.PathPatterns()...)
		}
	}
	snapName := appSet.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), appSet, opts)
	if err != nil {
		return fmt.Errorf("cannot obtain configfiles specification for snap %q: %s",
			snapName, err)
	}

	return b.ensureConfigfiles(spec.(*Specification), cfgPatterns)
}

// Remove removes modules configfiles files specific to a given snap.
// This method should be called after removing a snap.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Remove(snapName string) error {
	// If called for the system (snapd) snap, that is possible only in a
	// classic scenario when all other snaps in the system must have been
	// removed already to allow the removal of the snapd snap. In that
	// case, the config files will have already been removed by a Setup
	// call, so we do not need to do anything here.

	// TODO but this needs to be revisited for when we start supporting
	// configfiles plugs in snaps.
	return nil
}

// NewSpecification returns a new specification associated with this backend.
func (b *Backend) NewSpecification(*interfaces.SnapAppSet,
	interfaces.ConfinementOptions) interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of features supported by snapd for configfiles.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-configfiles"}
}

func (b *Backend) ensureConfigfiles(spec *Specification, cfgPatterns []string) error {
	// Setup configfiles only if the snap has plugs that require it. For the
	// moment this is only the system snap.
	if len(spec.plugs) == 0 {
		return nil
	}

	// Configuration files are created only if the files in the spec match
	// the patterns registered by interfaces.
	writtenPaths := make(map[string]bool, len(spec.pathContent))
	for path := range spec.pathContent {
		writtenPaths[path] = false
	}
	// TODO supported patterns apply currently only to a classic rootfs,
	// not to the rootfs of a snap. For the latter, the paths will be
	// relative to a directory in /var/lib/snapd/configfiles/<snap_name>/.
	// Files in there would be bind mounted so they can be seen by the
	// snap.
	for _, pattern := range cfgPatterns {
		matched := map[string]osutil.FileState{}
		for path, fileState := range spec.pathContent {
			match, err := filepath.Match(pattern, path)
			if err != nil {
				return fmt.Errorf("internal error in configfiles: %w", err)
			}
			if !match {
				continue
			}
			matched[filepath.Base(path)] = fileState
			writtenPaths[path] = true
		}
		targetDir := filepath.Dir(pattern)
		if len(matched) > 0 {
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("cannot create directory %q: %v", targetDir, err)
			}
		}
		// Note that this still needs to run if there are no matches, to remove files
		if _, _, err := osutil.EnsureDirState(targetDir,
			filepath.Base(pattern), matched); err != nil {
			return fmt.Errorf("cannot ensure state for %s files: %w", pattern, err)
		}
	}

	notMatched := []string{}
	for path, written := range writtenPaths {
		if written {
			continue
		}
		notMatched = append(notMatched, path)
	}
	if len(notMatched) > 0 {
		return fmt.Errorf("internal error: %v not in any registered configfiles pattern",
			notMatched)
	}

	return nil
}
