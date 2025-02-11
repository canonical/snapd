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

package ldconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/timings"
)

// Backend is responsible for maintaining ldconfig cache.
type Backend struct{}

var _ = interfaces.SecurityBackend(&Backend{})

// Initialize does nothing for this backend.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return "ldconfig"
}

// Setup will make the ldconfig backend generate the needed
// configuration files and re-create the ld cache.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	// For the moment only the system snap is supported - the
	// snap.system.conf file is owned by it and the set-up of other snaps
	// must not affect it.
	if !interfaces.IsTheSystemSnap(appSet.InstanceName()) {
		return nil
	}
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), appSet, opts)
	if err != nil {
		return fmt.Errorf("cannot obtain ldconfig specification for snap %q: %s",
			appSet.InstanceName(), err)
	}

	return b.setupLdconfigCache(spec.(*Specification))
}

// Remove removes modules ldconfig files specific to a given snap.
// This method should be called after removing a snap.
//
// If the method fails it should be re-tried (with a sensible strategy) by the caller.
func (b *Backend) Remove(snapName string) error {
	// If called for the system (snapd) snap, that is possible only in a
	// classic scenario when all other snaps in the system must have been
	// removed already to allow the removal of the snapd snap. In that
	// case, /etc/ld.so.conf.d/snap.system.conf will have already been
	// removed by a Setup call, so we do not need to do anything here.

	// TODO but this needs to be revisited for when we start supporting
	// ldconfig plugs in snaps.
	return nil
}

// NewSpecification returns a new specification associated with this backend.
func (b *Backend) NewSpecification(*interfaces.SnapAppSet,
	interfaces.ConfinementOptions) interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of features supported by snapd for ldconfig.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-ldconfig"}
}

func (b *Backend) setupLdconfigCache(spec *Specification) error {
	// TODO this considers only the case when the libraries are exposed to
	// the rootfs. For snaps, we will create files in
	// /var/lib/snapd/ldconfig/ that will be used to generate a cache
	// specific to each snap.

	// We only need one file per plug (we are considering only the system
	// plug atm), that will contain information for all connected slots -
	// the specification is recreated with all the information even if we
	// are refreshing only one of the snaps providing slots.

	ldConfigDir := dirs.SnapLdconfigDir
	ldconfigPath := filepath.Join(ldConfigDir, "snap.system.conf")
	runLdconfig := false
	if len(spec.libDirs) > 0 {
		// Sort the map, we want the content of the config file to be
		// deterministic to be able to detect changes and run ldconfig
		// only when necessary.
		type SnapSlotLibs struct {
			snapSlot SnapSlot
			dirs     []string
		}
		sortedSnapSlots := make([]SnapSlotLibs, 0, len(spec.libDirs))
		for key, dirs := range spec.libDirs {
			sortedSnapSlots = append(sortedSnapSlots, SnapSlotLibs{key, dirs})
		}
		sort.Slice(sortedSnapSlots, func(i, j int) bool {
			n1 := sortedSnapSlots[i].snapSlot.SnapName + "_" + sortedSnapSlots[i].snapSlot.SlotName
			n2 := sortedSnapSlots[j].snapSlot.SnapName + "_" + sortedSnapSlots[j].snapSlot.SlotName
			return n1 < n2
		})
		content := "## This file is automatically generated by snapd\n"
		for _, ssLibs := range sortedSnapSlots {
			content += fmt.Sprintf("\n# Directories from %s snap, %s slot\n",
				ssLibs.snapSlot.SnapName, ssLibs.snapSlot.SlotName)
			content += strings.Join(ssLibs.dirs, "\n")
			content += "\n"
		}

		if err := os.MkdirAll(ldConfigDir, 0755); err != nil {
			return fmt.Errorf("cannot create directory for ldconfig files %q: %s", ldConfigDir, err)
		}
		err := osutil.EnsureFileState(ldconfigPath, &osutil.MemoryFileState{
			Content: []byte(content),
			Mode:    0644,
		})
		if err != nil {
			if err != osutil.ErrSameState {
				return err
			}
			// No change in content, no need to run ldconfig
		} else {
			runLdconfig = true
		}
	} else if osutil.FileExists(ldconfigPath) {
		// All lib dirs have been removed, remove the file and run ldconfig
		if err := os.Remove(ldconfigPath); err != nil {
			return err
		}
		runLdconfig = true
	} // else: no lib dirs and no file, no change then, don't run ldconfig

	// Re-create cache when needed
	if runLdconfig {
		out, stderr, err := osutil.RunSplitOutput("ldconfig")
		if err != nil {
			return osutil.OutputErrCombine(out, stderr, err)
		}
	}

	return nil
}
