// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
)

func appendWithPrefix(paths []string, prefix string, filenames []string) []string {
	for _, filename := range filenames {
		paths = append(paths, filepath.Join(prefix, filename))
	}
	return paths
}

func removeEmptyDirs(baseDir, relPath string) error {
	for relPath != "." {
		mylog.Check(os.Remove(filepath.Join(baseDir, relPath)))
		// If the directory doesn't exist, then stop.

		// If the directory is not empty, then stop.

		relPath = filepath.Dir(relPath)
	}
	return nil
}

func matchAnyComponent(globs []string, path string) (ok bool, index int) {
	for path != "." {
		component := filepath.Base(path)
		if ok, index, _ = matchAny(globs, component); ok {
			return ok, index
		}
		path = filepath.Dir(path)
	}
	return false, 0
}

// EnsureTreeState ensures that a directory tree content matches expectations.
//
// EnsureTreeState walks subdirectories of the base directory, and
// uses EnsureDirStateGlobs to synchronise content with the
// corresponding entry in the content map.  Any non-existent
// subdirectories in the content map will be created.
//
// After synchronising all subdirectories, any subdirectories where
// files were removed that are now empty will itself be removed, plus
// its parent directories up to but not including the base directory.
//
// While there is a quick check to prevent creation of directories
// that match the file glob pattern, it is the caller's responsibility
// to not create directories that may match globs passed to other
// invocations.
//
// For example, if the glob "snap.$SNAP_NAME.*" is used then the
// caller should avoid trying to populate any directories matching
// "snap.*".
//
// If an error occurs, all matching files are removed from the tree.
//
// A list of changed and removed files is returned, as relative paths
// to the base directory.
func EnsureTreeState(baseDir string, globs []string, content map[string]map[string]FileState) (changed, removed []string, err error) {
	// Validity check globs before doing anything
	_, index := mylog.Check3(matchAny(globs, "foo"))

	// Validity check directory paths and file names in content dict
	for relPath, dirContent := range content {
		if filepath.IsAbs(relPath) {
			return nil, nil, fmt.Errorf("internal error: EnsureTreeState got absolute directory %q", relPath)
		}
		if ok, index := matchAnyComponent(globs, relPath); ok {
			return nil, nil, fmt.Errorf("internal error: EnsureTreeState got path %q that matches glob pattern %q", relPath, globs[index])
		}
		for baseName := range dirContent {
			if filepath.Base(baseName) != baseName {
				return nil, nil, fmt.Errorf("internal error: EnsureTreeState got filename %q in %q, which has a path component", baseName, relPath)
			}
			if ok, _, _ := matchAny(globs, baseName); !ok {
				return nil, nil, fmt.Errorf("internal error: EnsureTreeState got filename %q in %q, which doesn't match any glob patterns %q", baseName, relPath, globs)
			}
		}
	}
	// Find all existing subdirectories under the base dir.  Don't
	// perform any modifications here because, as it may confuse
	// Walk().
	subdirs := make(map[string]bool)
	mylog.Check(filepath.Walk(baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if !fileInfo.IsDir() {
			return nil
		}
		relPath := mylog.Check2(filepath.Rel(baseDir, path))

		subdirs[relPath] = true
		return nil
	}))

	// Ensure we process directories listed in content
	for relPath := range content {
		subdirs[relPath] = true
	}

	maybeEmpty := []string{}

	var firstErr error
	for relPath := range subdirs {
		dirContent := content[relPath]
		path := filepath.Join(baseDir, relPath)
		mylog.Check(os.MkdirAll(path, 0755))

		dirChanged, dirRemoved := mylog.Check3(EnsureDirStateGlobs(path, globs, dirContent))
		changed = appendWithPrefix(changed, relPath, dirChanged)
		removed = appendWithPrefix(removed, relPath, dirRemoved)

		if len(removed) != 0 {
			maybeEmpty = append(maybeEmpty, relPath)
		}
	}
	// As with EnsureDirState, if an error occurred we want to
	// delete all matching files under the whole baseDir
	// hierarchy.  This also means emptying subdirectories that
	// were successfully synchronised.
	if firstErr != nil {
		// changed paths will be deleted by this next step
		changed = nil
		for relPath := range subdirs {
			path := filepath.Join(baseDir, relPath)
			if !IsDirectory(path) {
				continue
			}
			_, dirRemoved, _ := EnsureDirStateGlobs(path, globs, nil)
			removed = appendWithPrefix(removed, relPath, dirRemoved)
			if len(removed) != 0 {
				maybeEmpty = append(maybeEmpty, relPath)
			}
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)

	// For directories where files were removed, attempt to remove
	// empty directories.
	for _, relPath := range maybeEmpty {
		mylog.Check(removeEmptyDirs(baseDir, relPath))
	}
	return changed, removed, firstErr
}
