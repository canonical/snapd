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

package wrappers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func findIconFiles(snapName string, rootDir string) (icons []string, err error) {
	if !osutil.IsDirectory(rootDir) {
		return nil, nil
	}
	iconGlob := fmt.Sprintf("snap.%s.*", snapName)
	forbiddenDirGlob := "snap.*"
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		if info.IsDir() {
			// Ignore directories that could match an icon glob
			if ok, err := filepath.Match(forbiddenDirGlob, base); ok || err != nil {
				return filepath.SkipDir
			}
		} else {
			if ok, err := filepath.Match(iconGlob, base); err != nil {
				return err
			} else if ok {
				ext := filepath.Ext(base)
				if ext == ".png" || ext == ".svg" {
					icons = append(icons, rel)
				}
			}
		}
		return nil
	})
	return icons, err
}

func deriveIconContent(instanceName string, rootDir string, icons []string) (content map[string]map[string]osutil.FileState, err error) {
	content = make(map[string]map[string]osutil.FileState)
	snapPrefix := fmt.Sprintf("snap.%s.", snap.InstanceSnap(instanceName))
	instancePrefix := fmt.Sprintf("snap.%s.", instanceName)

	for _, iconFile := range icons {
		base := filepath.Base(iconFile)
		if !strings.HasPrefix(base, snapPrefix) {
			return nil, fmt.Errorf("cannot use icon file %q: must start with snap prefix %q", iconFile, snapPrefix)
		}
		dir := filepath.Dir(iconFile)
		dirContent := content[dir]
		if dirContent == nil {
			dirContent = make(map[string]osutil.FileState)
			content[dir] = dirContent
		}
		// rename icons to match snap instance name
		base = instancePrefix + base[len(snapPrefix):]
		dirContent[base] = &osutil.FileReferencePlusMode{
			FileReference: osutil.FileReference{Path: filepath.Join(rootDir, iconFile)},
			Mode:          0644,
		}
	}
	return content, nil
}

// EnsureSnapIcons puts in place the icon files for the applications from the snap.
//
// It also removes icon files from the applications of the old snap revision to ensure
// that only new snap icon files exist.
func EnsureSnapIcons(s *snap.Info) error {
	if s == nil {
		return fmt.Errorf("internal error: snap info cannot be nil")
	}
	if err := os.MkdirAll(dirs.SnapDesktopIconsDir, 0755); err != nil {
		return err
	}

	rootDir := filepath.Join(s.MountDir(), "meta", "gui", "icons")
	icons, err := findIconFiles(s.SnapName(), rootDir)
	if err != nil {
		return err
	}

	content, err := deriveIconContent(s.InstanceName(), rootDir, icons)
	if err != nil {
		return err
	}
	iconGlob := fmt.Sprintf("snap.%s.*", s.InstanceName())
	_, _, err = osutil.EnsureTreeState(dirs.SnapDesktopIconsDir, []string{iconGlob}, content)
	return err
}

// RemoveSnapIcons removes the added icons for the applications in the snap.
func RemoveSnapIcons(s *snap.Info) error {
	if !osutil.IsDirectory(dirs.SnapDesktopIconsDir) {
		return nil
	}
	iconGlob := fmt.Sprintf("snap.%s.*", s.InstanceName())
	_, _, err := osutil.EnsureTreeState(dirs.SnapDesktopIconsDir, []string{iconGlob}, nil)
	return err
}
