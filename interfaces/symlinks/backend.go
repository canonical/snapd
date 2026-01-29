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

// symlinks is a backend that ensures that configuration files required by
// interfaces are present in the system. Currently it works only on classic and
// modifies the classic rootfs.
package symlinks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/timings"
)

// Backend is responsible for maintaining symlinks cache.
type Backend struct{}

var _ = interfaces.SecurityBackend(&Backend{})

// Initialize does nothing for this backend.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecuritySymlinks
}

// Setup will make the symlinks backend generate the specified symlinks.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository, tm timings.Measurer) error {
	symlinkDirs := map[string]bool{}
	for _, iface := range repo.AllInterfaces() {
		if symlnIface, ok := iface.(interfaces.SymlinksUser); ok {
			for _, d := range symlnIface.TrackedDirectories() {
				symlinkDirs[d] = true
			}
		}
	}
	snapName := appSet.InstanceName()
	// Get the spec that applies to this snap
	spec, err := repo.SnapSpecification(b.Name(), appSet, opts)
	if err != nil {
		return fmt.Errorf("cannot obtain symlinks specification for snap %q: %s",
			snapName, err)
	}

	return b.ensureSymlinks(spec.(*Specification), symlinkDirs)
}

// Remove removes modules symlinks files specific to a given snap.
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	// If called for the system (snapd) snap, that is possible only in a
	// classic scenario when all other snaps in the system must have been
	// removed already to allow the removal of the snapd snap. In that
	// case, the config files will have already been removed by a Setup
	// call, so we do not need to do anything here.

	// TODO but this needs to be revisited for when we start supporting
	// symlinks plugs in snaps.
	return nil
}

// NewSpecification returns a new specification associated with this backend.
func (b *Backend) NewSpecification(*interfaces.SnapAppSet,
	interfaces.ConfinementOptions) interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of features supported by snapd for symlinks.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-symlinks"}
}

func (b *Backend) removeUnwantedInDir(dir string, entries []os.DirEntry, activeLinks SymlinkToTarget) error {
	// Loop to remove unwanted symlinks
	for _, node := range entries {
		if node.Type() != fs.ModeSymlink {
			continue
		}
		path := filepath.Join(dir, node.Name())
		controlled, err := linkIsSnapdControlled(path)
		if err != nil {
			return err
		}
		if !controlled {
			continue
		}
		// link is active
		if _, ok := activeLinks[node.Name()]; ok {
			continue
		}

		// link not active, remove
		if err := os.Remove(path); err != nil {
			logger.Noticef("symlinks backend cannot remove %q", path)
		}
	}

	return nil
}

func (b *Backend) ensureSymlinks(spec *Specification, symlinkDirs map[string]bool) error {
	// Setup symlinks only if the snap has plugs that require it. For the
	// moment this is only the system snap.
	if len(spec.plugs) == 0 {
		return nil
	}

	for lnDir := range spec.dirsToLinkToTarget {
		if _, ok := symlinkDirs[lnDir]; !ok {
			return fmt.Errorf("internal error: %s not in any registered symlinks directory", lnDir)
		}
	}

	// TODO supported directories apply currently only to a classic rootfs,
	// not to the rootfs of a snap.
	for dir := range symlinkDirs {
		entries, err := os.ReadDir(dir)
		dirExists := true
		if err != nil {
			var pathErr *os.PathError
			if errors.As(err, &pathErr) {
				dirExists = false
			} else {
				return err
			}
		}
		lnsToTarget := spec.dirsToLinkToTarget[dir]

		// Remove now unwanted symlinks
		if err := b.removeUnwantedInDir(dir, entries, lnsToTarget); err != nil {
			return err
		}

		// Ensure state of the links we want in this directory
		if len(lnsToTarget) > 0 {
			if !dirExists {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return err
				}
			}
			for ln, target := range lnsToTarget {
				lnPath := filepath.Join(dir, ln)
				logger.Debugf("ensuring symlink %q -> %q", lnPath, target)
				osutil.EnsureFileState(lnPath, &osutil.SymlinkFileState{Target: target})
			}
		}
	}

	return nil
}

func linkIsSnapdControlled(symlinkPath string) (bool, error) {
	dest, err := os.Readlink(symlinkPath)
	if err != nil {
		return false, err
	}
	// If dest is under $SNAP or points to snaps data snapd owns the
	// symlink, otherwise it is not under snapd control (these symlinks are
	// absolute).
	//
	// TODO an alternative would be to add trusted extended attributes to
	// these symlinks (xattr(7)) - adding with the equivalent of "sudo
	// setfattr -h -n trusted.<snap> -v <val> <symlink>".
	return strings.HasPrefix(dest, dirs.SnapMountDir+"/") ||
		strings.HasPrefix(dest, dirs.SnapDataDir+"/"), nil
}
