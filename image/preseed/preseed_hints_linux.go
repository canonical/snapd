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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/logger"
)

// sysfsRootDir is the root the live sysfs tree is read from. It is a variable
// so that tests can point it at a directory mimicking a device.
var sysfsRootDir = "/"

// sysfsCapture accumulates the sysfs topology of the running system as it is
// discovered. Entries are collected in sets so that the same path reached
// through several class links is only recorded once.
type sysfsCapture struct {
	// root is the resolved directory the live sysfs tree is read from,
	// "/" in production
	root string

	dirs  map[string]bool
	files map[string]bool
	links map[string]string
}

func newSysfsCapture() (*sysfsCapture, error) {
	// resolve the root once: a test root, and in principle a production
	// one, may itself contain symlinks which must not end up in the
	// logical paths
	root, err := filepath.EvalSymlinks(sysfsRootDir)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve sysfs root %q: %v", sysfsRootDir, err)
	}
	return &sysfsCapture{
		root:  root,
		dirs:  make(map[string]bool),
		files: make(map[string]bool),
		links: make(map[string]string),
	}, nil
}

// sourcePath returns the live path to read for the sysfs path relPath, for
// example "sys/class/leds" under the capture root.
func (sc *sysfsCapture) sourcePath(relPath string) string {
	return filepath.Join(sc.root, relPath)
}

// logicalPath converts p, a path under the capture root, into the
// slash-separated path relative to "/" used in hints documents, for example
// "sys/class/leds". This keeps the capture root out of the produced document.
func (sc *sysfsCapture) logicalPath(p string) (string, error) {
	rel, err := filepath.Rel(sc.root, p)
	if err != nil {
		return "", fmt.Errorf("cannot determine the sysfs path of %q: %v", p, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("cannot capture %q: outside of the sysfs root %q", p, sc.root)
	}
	return rel, nil
}

func (sc *sysfsCapture) addDir(p string) error {
	logical, err := sc.logicalPath(p)
	if err != nil {
		return err
	}
	sc.dirs[logical] = true
	return nil
}

func (sc *sysfsCapture) addFile(p string) error {
	logical, err := sc.logicalPath(p)
	if err != nil {
		return err
	}
	sc.files[logical] = true
	return nil
}

func (sc *sysfsCapture) addLink(p, target string) error {
	logical, err := sc.logicalPath(p)
	if err != nil {
		return err
	}
	sc.links[logical] = target
	return nil
}

// hints returns the captured topology with deterministically ordered entries,
// or nil if nothing relevant was found on the system.
func (sc *sysfsCapture) hints() *sysfsHints {
	if len(sc.dirs) == 0 && len(sc.files) == 0 && len(sc.links) == 0 {
		return nil
	}

	hints := &sysfsHints{
		Directories: sortedPaths(sc.dirs),
		Files:       sortedPaths(sc.files),
	}
	if paths := sortedPaths(sc.links); len(paths) > 0 {
		hints.Symlinks = make([]symlinkHint, 0, len(paths))
		for _, p := range paths {
			hints.Symlinks = append(hints.Symlinks, symlinkHint{
				Path:   p,
				Target: sc.links[p],
			})
		}
	}
	return hints
}

// captureFiles records the regular files found directly in dir, at the path
// the system resolves them to.
func (sc *sysfsCapture) captureFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", dir, err)
	}

	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		p, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
		if err != nil {
			// vanished or unreadable, nothing to record
			continue
		}
		if err := sc.addFile(p); err != nil {
			return err
		}
	}

	return nil
}

// captureLinks records the symlinks found directly in dir, preserving their
// raw targets.
func (sc *sysfsCapture) captureLinks(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", dir, err)
	}

	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		linkPath := filepath.Join(dir, e.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			return fmt.Errorf("cannot read symlink %q: %v", linkPath, err)
		}
		if err := sc.addLink(linkPath, target); err != nil {
			return err
		}
	}

	return nil
}

// captureSubdirs records the subdirectories found directly in dir, at the path
// the system resolves them to, together with their direct regular files.
func (sc *sysfsCapture) captureSubdirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", dir, err)
	}

	for _, e := range entries {
		if !e.Type().IsDir() {
			continue
		}
		p, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if err := sc.addDir(p); err != nil {
			return err
		}
		if err := sc.captureFiles(p); err != nil {
			return err
		}
	}

	return nil
}

// captureClassDir records classDir itself, the backing device directory behind
// each of its class symlinks, and the direct entries of both. An empty class
// directory is recorded as well, its mere existence matters to the interfaces
// looking at it.
func (sc *sysfsCapture) captureClassDir(classDir string) error {
	if err := sc.addDir(classDir); err != nil {
		return err
	}

	entries, err := os.ReadDir(classDir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", classDir, err)
	}

	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		linkPath := filepath.Join(classDir, e.Name())

		// the backing device directory, as the system resolves it
		deviceDir, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			// dangling link, there is nothing to mirror behind it
			continue
		}

		// the raw target is preserved so that the overlay reproduces
		// the relative class links of the device verbatim
		target, err := os.Readlink(linkPath)
		if err != nil {
			return fmt.Errorf("cannot read symlink %q: %v", linkPath, err)
		}
		if err := sc.addLink(linkPath, target); err != nil {
			return err
		}
		if err := sc.addDir(deviceDir); err != nil {
			return err
		}

		if err := sc.captureSubdirs(deviceDir); err != nil {
			return err
		}
		if err := sc.captureFiles(deviceDir); err != nil {
			return err
		}
		if err := sc.captureLinks(deviceDir); err != nil {
			return err
		}
	}

	// regular files and subdirectories sitting directly in the class
	// directory rather than behind a class link
	if err := sc.captureFiles(classDir); err != nil {
		return err
	}
	return sc.captureSubdirs(classDir)
}

// captureSysfsHints inspects the permitted sys/class roots of the running
// system and returns the topology needed to reconstruct them elsewhere, or nil
// if nothing relevant was found.
func captureSysfsHints() (*sysfsHints, error) {
	sc, err := newSysfsCapture()
	if err != nil {
		return nil, err
	}

	for _, relPath := range permitedSysfsOverlays {
		// only the class roots are walked: the sys/devices paths are
		// captured as the real targets of the class symlinks, exactly
		// as the device exposes them
		if !strings.HasPrefix(relPath, "sys/class/") {
			continue
		}
		classDir := sc.sourcePath(relPath)
		if _, err := os.Lstat(classDir); err != nil {
			if os.IsNotExist(err) {
				// the class is not present on this system
				continue
			}
			return nil, fmt.Errorf("cannot stat %q: %v", classDir, err)
		}
		if err := sc.captureClassDir(classDir); err != nil {
			return nil, err
		}
	}

	return sc.hints(), nil
}

// WritePreseedHints captures the properties of the running system that are
// needed to preseed an image for it on a different build host, and writes them
// to hintsFile as a preseed hints document. The destination must not exist yet
// and nothing is left behind if the capture or the write fails.
func WritePreseedHints(hintsFile string) (err error) {
	f, err := os.OpenFile(hintsFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("preseed hints file %q already exists, remove it first", hintsFile)
		}
		return fmt.Errorf("cannot create preseed hints file: %v", err)
	}
	// err is a named return value, so this runs on every exit path and can
	// still change what the function returns
	defer func() {
		// writes are buffered, so a write failure may only surface when
		// closing; report it unless something already went wrong, in
		// which case that error is the more informative one
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("cannot write preseed hints file %q: %v", hintsFile, cerr)
		}
		if err != nil {
			// never leave a partially written hints file behind
			if e := os.Remove(hintsFile); e != nil && !os.IsNotExist(e) {
				logger.Noticef("cannot remove partially written preseed hints file %q: %v", hintsFile, e)
			}
		}
	}()

	sysfs, err := captureSysfsHints()
	if err != nil {
		return err
	}
	if sysfs != nil {
		// the produced document must be one this package can consume
		// again, rather than one rejected later at prepare-image time
		if err = validateSysfsHints(sysfs); err != nil {
			return fmt.Errorf("cannot capture sysfs hints: %v", err)
		}
	}

	// deterministic and human inspectable, with a final newline
	data, err := json.MarshalIndent(&hintsDoc{
		Format: preseedHintsFormat,
		Sysfs:  sysfs,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize preseed hints: %v", err)
	}
	data = append(data, '\n')

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("cannot write preseed hints file %q: %v", hintsFile, err)
	}

	// the deferred close reports any buffered write failure
	return nil
}
