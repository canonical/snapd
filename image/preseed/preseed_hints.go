// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package preseed

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/snapcore/snapd/logger"
)

// preseedHintsFormat is the version of the preseed hints document written and
// understood by this package. It is bumped whenever the meaning of the
// existing fields changes.
const preseedHintsFormat = 1

// hintsDoc is the on-disk representation of a preseed hints document. Hints
// describe the properties of a target device that are needed to preseed an
// image for it on a build host that is not the device itself. Namespaces are
// versioned as a whole through the top level format field.
type hintsDoc struct {
	// Format is the hints document version, see preseedHintsFormat.
	Format int `json:"format"`
	// Sysfs describes the sysfs topology of the target device. It is
	// absent when nothing relevant was found on the device.
	Sysfs *sysfsHints `json:"sysfs,omitempty"`
}

// sysfsHints captures the parts of the sysfs tree of the target device that
// snap interface security profile generation looks at. All paths are
// slash-separated and relative to "/", for example "sys/class/leds".
type sysfsHints struct {
	// Directories are the directories to create, sorted.
	Directories []string `json:"directories,omitempty"`
	// Files are the files to create empty, sorted.
	Files []string `json:"files,omitempty"`
	// Symlinks are the symlinks to create, sorted by path.
	Symlinks []symlinkHint `json:"symlinks,omitempty"`
}

// symlinkHint describes one symlink of the sysfs tree of the target device.
type symlinkHint struct {
	// Path is where the symlink is created.
	Path string `json:"path"`
	// Target is the raw link target as read from the target device. It is
	// reproduced verbatim and deliberately not resolved: sysfs class links
	// are relative and the consumers of the overlay resolve them
	// themselves.
	Target string `json:"target"`
}

// readPreseedHints reads and strictly decodes the single hints document in
// hintsFile, rejecting unknown fields and unsupported format versions.
func readPreseedHints(hintsFile string) (*hintsDoc, error) {
	f, err := os.Open(hintsFile)
	if err != nil {
		return nil, fmt.Errorf("cannot open preseed hints file: %v", err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()

	var doc hintsDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("cannot parse preseed hints file %q: %v", hintsFile, err)
	}
	// a hints file carries exactly one document
	if dec.More() {
		return nil, fmt.Errorf("cannot parse preseed hints file %q: unexpected content after the hints document", hintsFile)
	}

	switch doc.Format {
	case 0:
		return nil, fmt.Errorf("cannot use preseed hints file %q: missing format version", hintsFile)
	case preseedHintsFormat:
	default:
		return nil, fmt.Errorf("cannot use preseed hints file %q: unsupported format version %d, expected %d", hintsFile, doc.Format, preseedHintsFormat)
	}

	return &doc, nil
}

// sortedPaths returns the keys of set in ascending order, or nil if empty.
func sortedPaths[V any](set map[string]V) []string {
	if len(set) == 0 {
		return nil
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// sysfsEntryKind tells what kind of entry a hints path creates in the overlay.
type sysfsEntryKind int

const (
	sysfsDirEntry sysfsEntryKind = iota
	sysfsFileEntry
	sysfsSymlinkEntry
)

func (k sysfsEntryKind) String() string {
	switch k {
	case sysfsDirEntry:
		return "directory"
	case sysfsFileEntry:
		return "file"
	case sysfsSymlinkEntry:
		return "symlink"
	}
	return fmt.Sprintf("entry kind %d", int(k))
}

// validateSysfsHintPath checks that p is a clean, slash-separated path
// relative to "/", for example "sys/class/leds", so that it can only ever
// name an entry inside the overlay directory.
func validateSysfsHintPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("empty path")
	case path.IsAbs(p):
		return fmt.Errorf("invalid path %q: must be relative to /", p)
	case p == ".":
		return fmt.Errorf("invalid path %q: must name an entry", p)
	case p == ".." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("invalid path %q: must not escape the overlay", p)
	case p != path.Clean(p):
		return fmt.Errorf("invalid path %q: must be clean", p)
	case strings.ContainsRune(p, '\\'):
		return fmt.Errorf("invalid path %q: must not contain backslashes", p)
	}
	return nil
}

// validateSysfsHints checks that every path in hints can be safely created
// under a fresh overlay directory. It rejects paths that are not clean
// relative paths, duplicates, paths claimed by more than one kind of entry,
// and paths nested under a file or a symlink, which could otherwise make
// overlay creation write outside of the overlay directory.
func validateSysfsHints(hints *sysfsHints) error {
	kinds := make(map[string]sysfsEntryKind, len(hints.Directories)+len(hints.Files)+len(hints.Symlinks))

	add := func(p string, kind sysfsEntryKind) error {
		if err := validateSysfsHintPath(p); err != nil {
			return err
		}
		if previous, ok := kinds[p]; ok {
			if previous == kind {
				return fmt.Errorf("duplicated %s %q", kind, p)
			}
			return fmt.Errorf("conflicting entries for %q: %s and %s", p, previous, kind)
		}
		kinds[p] = kind
		return nil
	}

	for _, dir := range hints.Directories {
		if err := add(dir, sysfsDirEntry); err != nil {
			return err
		}
	}
	for _, file := range hints.Files {
		if err := add(file, sysfsFileEntry); err != nil {
			return err
		}
	}
	for _, link := range hints.Symlinks {
		if err := add(link.Path, sysfsSymlinkEntry); err != nil {
			return err
		}
		if link.Target == "" {
			return fmt.Errorf("empty target for symlink %q", link.Path)
		}
	}

	// only directories can have entries nested under them: symlinks are
	// created last and are never followed while creating the overlay
	for _, p := range sortedPaths(kinds) {
		for parent := path.Dir(p); parent != "."; parent = path.Dir(parent) {
			if kind, ok := kinds[parent]; ok && kind != sysfsDirEntry {
				return fmt.Errorf("entry %q is nested under %s %q", p, kind, parent)
			}
		}
	}

	return nil
}

// makeOverlayTempDir creates the temporary directory backing a materialized
// sysfs overlay. It is a variable so that tests can inject failures.
var makeOverlayTempDir = func() (string, error) {
	return os.MkdirTemp("", "snapd-sysfs-overlay-")
}

// makeOverlayCleanup returns an idempotent function removing overlayDir.
func makeOverlayCleanup(overlayDir string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := os.RemoveAll(overlayDir); err != nil {
				logger.Noticef("cannot remove temporary sysfs overlay %q: %v", overlayDir, err)
			}
		})
	}
}

// createSysfsOverlay populates overlayDir following hints, which must have
// been validated already. Directories and files are created before symlinks
// so that no overlay path is ever resolved through a symlink coming from the
// hints document.
func createSysfsOverlay(overlayDir string, hints *sysfsHints) error {
	// hints paths are slash-separated and relative to "/", so they are
	// manipulated with path and only turned into filesystem paths, with
	// filepath, when an entry is actually created
	mkdirAll := func(hintsDir string) error {
		if err := os.MkdirAll(filepath.Join(overlayDir, hintsDir), 0755); err != nil {
			return fmt.Errorf("cannot create sysfs overlay directory %q: %v", hintsDir, err)
		}
		return nil
	}

	for _, dir := range hints.Directories {
		if err := mkdirAll(dir); err != nil {
			return err
		}
	}

	for _, file := range hints.Files {
		if err := mkdirAll(path.Dir(file)); err != nil {
			return err
		}
		// the entries of the target device are only mirrored as empty
		// stubs, their content is never relevant for preseeding, so the
		// file is created but never written to
		f, err := os.OpenFile(filepath.Join(overlayDir, file), os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return fmt.Errorf("cannot create sysfs overlay file %q: %v", file, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("cannot create sysfs overlay file %q: %v", file, err)
		}
	}

	for _, link := range hints.Symlinks {
		if err := mkdirAll(path.Dir(link.Path)); err != nil {
			return err
		}
		if err := os.Symlink(link.Target, filepath.Join(overlayDir, link.Path)); err != nil {
			return fmt.Errorf("cannot create sysfs overlay symlink %q: %v", link.Path, err)
		}
	}

	return nil
}

// CreateSysfsOverlayFromHints materializes the sysfs part of the preseed hints
// document in hintsFile as a temporary sysfs overlay directory, suitable for
// image.Options.SysfsOverlay. On success it returns the overlay directory and
// an idempotent cleanup function which the caller must invoke once it is done
// with the overlay. Nothing is left behind when an error is returned.
func CreateSysfsOverlayFromHints(hintsFile string) (overlayDir string, cleanup func(), err error) {
	doc, err := readPreseedHints(hintsFile)
	if err != nil {
		return "", nil, err
	}

	hints := doc.Sysfs
	if hints == nil {
		hints = &sysfsHints{}
	}
	if err := validateSysfsHints(hints); err != nil {
		return "", nil, fmt.Errorf("cannot use preseed hints file %q: %v", hintsFile, err)
	}

	overlayDir, err = makeOverlayTempDir()
	if err != nil {
		return "", nil, fmt.Errorf("cannot create sysfs overlay directory: %v", err)
	}
	cleanup = makeOverlayCleanup(overlayDir)

	if err := createSysfsOverlay(overlayDir, hints); err != nil {
		// never leave a partial overlay behind
		cleanup()
		return "", nil, err
	}

	return overlayDir, cleanup, nil
}
