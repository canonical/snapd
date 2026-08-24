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

package export

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/osutil"
)

// ensureExports reconciles the on-disk export tree with what is declared in
// spec, for every interface in exportIfaces (every interface using the
// export backend, whether or not it currently has any connections - see
// Setup).
//
// Like the symlinks and configfiles backends, this is deliberately dumb: it
// trusts that spec reflects the complete, current desired state (built by
// walking every connection of the system snap's plugs, so a single call
// yields the full state, not a delta - see
// Repository.SnapSpecification), and it does not defend against snap
// content being unavailable when it runs; that precondition is established
// by the caller (see interfaces/ifacestate), not by this backend.
func (b *Backend) ensureExports(spec *Specification, exportIfaces map[string]bool) error {
	// Setup exports only if the snap has plugs that require it. For the
	// moment this is only the system snap.
	if len(spec.plugs) == 0 {
		return nil
	}

	files := spec.Files()
	for ifaceName := range exportIfaces {
		if err := ensureExportsForInterface(ifaceName, files[ifaceName]); err != nil {
			return fmt.Errorf("cannot ensure export state for interface %q: %w", ifaceName, err)
		}
	}
	return nil
}

// ensureExportsForInterface reconciles the on-disk tree at InterfaceRoot(ifaceName)
// with desired, which maps unit name (see UnitName) to the files that unit
// must contain (path relative to the unit, see AddExportedFile). A nil or
// empty desired means the interface currently has nothing to export (no
// connections, or none of them declared any files), and the entire tree for
// it is removed.
//
// The steps (see the design's "Write sequence"):
//  1. materialise every unit not already present into a temporary directory,
//     then atomically rename it into place - a directory rename is atomic,
//     writing N files into a directory is not, so this avoids ever exposing
//     a half-populated unit to a reader;
//  2. atomically flip the export.sources manifest to list every file in the
//     desired state;
//  3. remove anything under the interface root that is not a desired unit
//     and not the manifest - this is unconditionally correct garbage
//     collection: it covers disconnect, snap removal, refresh, revert,
//     component refresh, and leftover "<unit>.tmp" directories from a
//     previous, interrupted run, all with the same rule, without needing to
//     know why something became unreferenced.
//
// Collection in step 3 is deliberately immediate, in the same pass that
// flipped the manifest, rather than being deferred to a later Setup() or
// held back by a grace period. The consequence is that a reader which has
// already read the manifest may find a file it lists gone, or a unit being
// removed while it walks it. Readers are therefore *required* to be
// resilient: skip files that cannot be found or cannot be read, rather than
// treating either as fatal. This is a contract, not an implementation
// detail - snap-confine's consumer of this tree honours it via
// sc_do_optional_mount(), which silently no-ops on a missing source instead
// of failing the snap launch.
func ensureExportsForInterface(ifaceName string, desired map[string]map[string]osutil.FileState) error {
	root := InterfaceRoot(ifaceName)

	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		entries = nil
	}

	if len(desired) > 0 {
		if err := os.MkdirAll(root, 0755); err != nil {
			return fmt.Errorf("cannot create %q: %w", root, err)
		}
	}

	for unit, unitFiles := range desired {
		// Unit names are derived from immutable snap/component
		// revisions (see UnitName), so if the unit directory already
		// exists it already has the right content - reverting to a
		// unit that was not yet garbage collected reuses it verbatim,
		// with no writes.
		if osutil.IsDirectory(UnitDir(ifaceName, unit)) {
			continue
		}
		if err := materialiseUnit(ifaceName, unit, unitFiles); err != nil {
			return err
		}
	}

	if err := writeManifest(ifaceName, desired); err != nil {
		return err
	}

	manifestName := filepath.Base(ManifestPath(ifaceName))
	for _, entry := range entries {
		name := entry.Name()
		if name == manifestName {
			continue
		}
		if _, ok := desired[name]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return err
		}
	}

	if len(desired) == 0 {
		// Nothing references this interface's tree any more; remove
		// it too, so a fully disconnected interface leaves no trace.
		// The directory is expected to be empty at this point (the
		// loop above just removed every entry, and the manifest was
		// removed by writeManifest); tolerate it not being empty or
		// not existing, since either just means there is nothing
		// further to do here.
		if err := os.Remove(root); err != nil &&
			!errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			return err
		}
	}

	return nil
}

// materialiseUnit writes files into a temporary directory for unit, then
// atomically renames it into place at UnitDir(ifaceName, unit).
//
// TODO: UnitTmpDir is a deterministic path derived from the unit name, so
// two Setup() calls racing on the same unit share it. Concurrent Setup() of
// the system snap is possible: setupSecurityByBackend drops the state lock
// for the whole backend loop (see overlord/ifacestate/helpers.go), and
// while connect/disconnect are serialised by CheckChangeConflictMany over
// {plugSnap, slotSnap}, two independent snap refreshes both reach
// setupAffectedSnaps for the system snap. The two racing calls can then
// interleave as: one RemoveAll's the other's in-progress temporary
// directory, or both reach AtomicRename and the loser fails with ENOTEMPTY
// (it is rename(2), which will not replace a non-empty directory).
//
// The result is a spurious, transient Setup() failure that heals on retry,
// not corrupt state: a unit name is derived from immutable revisions (see
// UnitName), so racing writers of the same unit are writing identical
// content. A related interleaving in ensureExportsForInterface can leave
// the manifest referencing a unit a concurrent stale-spec writer collected,
// which likewise resolves on the next Setup().
//
// Fix by giving each call a unique temporary directory (os.MkdirTemp-style)
// or by tolerating ENOTEMPTY/EEXIST from the rename after re-verifying that
// the target holds the expected content.
func materialiseUnit(ifaceName, unit string, files map[string]osutil.FileState) error {
	tmpDir := UnitTmpDir(ifaceName, unit)
	// Remove any stale, incomplete attempt left behind by a previous,
	// interrupted run before starting a fresh one.
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("cannot remove stale %q: %w", tmpDir, err)
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("cannot create %q: %w", tmpDir, err)
	}

	for relPath, state := range files {
		if err := writeUnitFile(tmpDir, relPath, state); err != nil {
			return fmt.Errorf("cannot write %q: %w", filepath.Join(unit, relPath), err)
		}
	}

	unitDir := UnitDir(ifaceName, unit)
	if err := osutil.AtomicRename(tmpDir, unitDir); err != nil {
		return fmt.Errorf("cannot rename %q to %q: %w", tmpDir, unitDir, err)
	}
	return nil
}

// writeUnitFile writes state's content to relPath under dir, creating any
// necessary subdirectories (relPath always has at least one - see
// AddExportedFile).
func writeUnitFile(dir, relPath string, state osutil.FileState) error {
	reader, _, mode, err := state.State()
	if err != nil {
		return err
	}
	defer reader.Close()

	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, reader)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// writeManifest atomically flips the export.sources manifest for ifaceName
// to list every file described by desired, one path relative to
// InterfaceRoot per line, sorted for a stable diff. If desired is empty, any
// existing manifest is removed instead of being replaced with an empty
// file, so a fully disconnected interface leaves no trace.
func writeManifest(ifaceName string, desired map[string]map[string]osutil.FileState) error {
	manifestPath := ManifestPath(ifaceName)

	if len(desired) == 0 {
		err := os.Remove(manifestPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}

	var lines []string
	for unit, files := range desired {
		for relPath := range files {
			lines = append(lines, filepath.Join(unit, relPath))
		}
	}
	sort.Strings(lines)
	content := strings.Join(lines, "\n") + "\n"

	err := osutil.EnsureFileState(manifestPath, &osutil.MemoryFileState{Content: []byte(content), Mode: 0644})
	if err != nil && !errors.Is(err, osutil.ErrSameState) {
		return err
	}
	return nil
}
