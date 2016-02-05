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
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
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
func NewLocalSnapRepository() *SnapLocalRepository {
	path := dirs.SnapSnapsDir
	if s, err := os.Stat(path); err != nil || !s.IsDir() {
		return nil
	}
	return &SnapLocalRepository{path: path}
}

// Installed returns the installed snaps from this repository
func (s *SnapLocalRepository) Installed() (parts []Part, err error) {
	globExpr := filepath.Join(s.path, "*", "*", "meta", "snap.yaml")
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
