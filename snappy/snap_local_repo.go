// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SideloadedOrigin is the (forced) origin for sideloaded snaps
	SideloadedOrigin = "sideload"
)

// SnapLocalRepository is the type for a local snap repository
type SnapLocalRepository struct {
	path string
}

// NewLocalSnapRepository returns a new SnapLocalRepository for the given
// path
func NewLocalSnapRepository(path string) *SnapLocalRepository {
	if s, err := os.Stat(path); err != nil || !s.IsDir() {
		return nil
	}
	return &SnapLocalRepository{path: path}
}

// Description describes the local repository
func (s *SnapLocalRepository) Description() string {
	return fmt.Sprintf("Snap local repository for %s", s.path)
}

// Details returns details for the given snap
func (s *SnapLocalRepository) Details(name string, origin string) (versions []Part, err error) {
	if origin == "" || origin == SideloadedOrigin {
		origin = "*"
	}
	appParts, err := s.partsForGlobExpr(filepath.Join(s.path, name+"."+origin, "*", "meta", "package.yaml"))
	fmkParts, err := s.partsForGlobExpr(filepath.Join(s.path, name, "*", "meta", "package.yaml"))

	parts := append(appParts, fmkParts...)

	if len(parts) == 0 {
		return nil, ErrPackageNotFound
	}

	return parts, nil
}

// Updates returns the available updates
func (s *SnapLocalRepository) Updates() (parts []Part, err error) {
	return nil, err
}

// Installed returns the installed snaps from this repository
func (s *SnapLocalRepository) Installed() (parts []Part, err error) {
	globExpr := filepath.Join(s.path, "*", "*", "meta", "package.yaml")
	return s.partsForGlobExpr(globExpr)
}

// All the parts (ie all installed + removed-but-not-purged)
//
// TODO: that thing about removed
func (s *SnapLocalRepository) All() ([]Part, error) {
	return s.Installed()
}
func (s *SnapLocalRepository) partsForGlobExpr(globExpr string) (parts []Part, err error) {
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return nil, err
	}

	for _, yamlfile := range matches {

		// skip "current" and similar symlinks
		realpath, err := filepath.EvalSymlinks(yamlfile)
		if err != nil {
			return nil, err
		}
		if realpath != yamlfile {
			continue
		}

		origin, _ := originFromYamlPath(realpath)
		snap, err := NewInstalledSnapPart(realpath, origin)
		if err != nil {
			return nil, err
		}
		parts = append(parts, snap)

	}

	return parts, nil
}

func originFromBasedir(basedir string) (s string) {
	ext := filepath.Ext(filepath.Dir(filepath.Clean(basedir)))
	if len(ext) < 2 {
		return ""
	}

	return ext[1:]
}

// originFromYamlPath *must* return "" if it's returning error.
func originFromYamlPath(path string) (string, error) {
	origin := originFromBasedir(filepath.Join(path, "..", ".."))

	if origin == "" {
		return "", ErrInvalidPart
	}

	return origin, nil
}
