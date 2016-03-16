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
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
)

const (
	// SideloadedDeveloper is the (forced) developer for sideloaded snaps
	SideloadedDeveloper = "sideload"
)

// SnapLocalRepository is the type for a local snap repository
type SnapLocalRepository struct {
	path string
}

// NewLocalSnapRepository returns a new SnapLocalRepository for the given
// path
func NewLocalSnapRepository() *SnapLocalRepository {
	return &SnapLocalRepository{
		path: dirs.SnapSnapsDir,
	}
}

// Installed returns the installed snaps from this repository
func (s *SnapLocalRepository) Installed() ([]*Snap, error) {
	globExpr := filepath.Join(s.path, "*", "*", "meta", "snap.yaml")
	return s.snapsForGlobExpr(globExpr)
}

// Snaps gets all the snaps with the given name and origin
func (s *SnapLocalRepository) Snaps(name, origin string) ([]*Snap, error) {
	globExpr := filepath.Join(s.path, name+"."+origin, "*", "meta", "snap.yaml")
	return s.snapsForGlobExpr(globExpr)
}

func (s *SnapLocalRepository) snapsForGlobExpr(globExpr string) (parts []*Snap, err error) {
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

		developer, _ := developerFromYamlPath(realpath)
		snap, err := NewInstalledSnap(realpath, developer)
		if err != nil {
			return nil, err
		}
		parts = append(parts, snap)

	}

	return parts, nil
}

func developerFromBasedir(basedir string) (s string) {
	ext := filepath.Ext(filepath.Dir(filepath.Clean(basedir)))
	if len(ext) < 2 {
		return ""
	}

	return ext[1:]
}

// developerFromYamlPath *must* return "" if it's returning error.
func developerFromYamlPath(path string) (string, error) {
	developer := developerFromBasedir(filepath.Join(path, "..", ".."))

	if developer == "" {
		return "", ErrInvalidPart
	}

	return developer, nil
}
