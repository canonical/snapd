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
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

func appendWithPrefix(paths []string, prefix string, filenames []string) []string {
	for _, filename := range filenames {
		paths = append(paths, filepath.Join(prefix, filename))
	}
	return paths
}

func removeEmptyDirs(baseDir, relPath string) error {
	for relPath != "." {
		if err := os.Remove(filepath.Join(baseDir, relPath)); err != nil {
			// If the directory doesn't exist, then stop.
			if os.IsNotExist(err) {
				return nil
			}
			// If the directory is not empty, then stop.
			if pathErr, ok := err.(*os.PathError); ok && pathErr.Err == syscall.ENOTEMPTY {
				return nil
			}
			return err
		}
		relPath = filepath.Dir(relPath)
	}
	return nil
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
// No checks are performed to see whether subdirectories match the
// passed globs, so it is the caller's responsibility to not create
// directories that may match any globs passed in.
//
// For example, if the glob "snap.$SNAP_NAME.*" is used then the
// caller should avoid trying to populate any directories matching
// "snap.*".
//
// A list of changed and removed files is returned, as relative paths
// to the base directory.
func EnsureTreeState(baseDir string, globs []string, content map[string]map[string]*FileState) (changed, removed []string, err error) {
	// Find all existing subdirectories under the base dir.  Don't
	// perform any modifications here because, as it may confuse
	// Walk().
	subdirs := make(map[string]bool)
	err = filepath.Walk(baseDir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fileInfo.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		subdirs[relPath] = true
		return nil
	})
	if err != nil {
		return changed, removed, err
	}
	// Ensure we process directories listed in content
	for relPath := range content {
		subdirs[relPath] = true
	}

	maybeEmpty := []string{}

	// TODO: ensure that no directories in the tree match a
	// directory glob (one that would match any globs that would
	// be passed in a particular context).
	for relPath := range subdirs {
		dirContent := content[relPath]
		path := filepath.Join(baseDir, relPath)
		if err = os.MkdirAll(path, 0755); err != nil {
			break
		}
		var dirChanged, dirRemoved []string
		dirChanged, dirRemoved, err = EnsureDirStateGlobs(path, globs, dirContent)
		changed = appendWithPrefix(changed, relPath, dirChanged)
		removed = appendWithPrefix(removed, relPath, dirRemoved)
		if err != nil {
			break
		}
		if len(removed) != 0 {
			maybeEmpty = append(maybeEmpty, relPath)
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)
	if err != nil {
		return changed, removed, err
	}

	// For directories where files were removed, attempt to remove
	// empty directories.
	for _, relPath := range maybeEmpty {
		if err = removeEmptyDirs(baseDir, relPath); err != nil {
			break
		}
	}
	return changed, removed, err
}
