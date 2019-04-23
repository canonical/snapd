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
)

func appendWithPrefix(paths []string, prefix string, filenames []string) []string {
	for _, filename := range filenames {
		paths = append(paths, filepath.Join(prefix, filename))
	}
	return paths
}

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
	}
	sort.Strings(changed)
	sort.Strings(removed)
	return changed, removed, err
}
