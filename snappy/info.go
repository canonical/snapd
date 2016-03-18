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
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

// SystemConfig is a config map holding configs for multiple packages
type SystemConfig map[string]interface{}

// Configuration allows requesting a gadget snappy package type's config
type Configuration interface {
	GadgetConfig() SystemConfig
}

// QualifiedName of a snap.Info is the Name, in most cases qualified with the
// Developer
func QualifiedName(p *snap.Info) string {
	if t := p.Type; t == snap.TypeGadget {
		return p.Name
	}
	return p.Name + "." + p.Developer
}

// BareName of a snap.Info is just its Name
func BareName(p *snap.Info) string {
	return p.Name
}

// FullName of a snap.Info is Name.Developer
func FullName(p *snap.Info) string {
	return p.Name + "." + p.Developer
}

// FullNameWithChannel returns the FullName, with the channel appended
// if it has one.
func fullNameWithChannel(p *snap.Info) string {
	name := FullName(p)
	ch := p.Channel
	if ch == "" {
		return name
	}

	return fmt.Sprintf("%s/%s", name, ch)
}

// ActiveSnapsByType returns all installed snaps with the given type
func ActiveSnapsByType(snapTs ...snap.Type) (res []*Snap, err error) {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return nil, err
	}

	for _, snap := range installed {
		if !snap.IsActive() {
			continue
		}
		for i := range snapTs {
			if snap.Type() == snapTs[i] {
				res = append(res, snap)
			}
		}
	}

	return res, nil
}

// ActiveSnapIterByType returns the result of applying the given
// function to all active snaps with the given type.
var ActiveSnapIterByType = activeSnapIterByTypeImpl

func activeSnapIterByTypeImpl(f func(*snap.Info) string, snapTs ...snap.Type) ([]string, error) {
	installed, err := ActiveSnapsByType(snapTs...)
	res := make([]string, len(installed))

	for i, snap := range installed {
		res[i] = f(snap.Info())
	}

	return res, err
}

// ActiveSnapByName returns all active snaps with the given name
func ActiveSnapByName(needle string) *Snap {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return nil
	}
	for _, snap := range installed {
		if !snap.IsActive() {
			continue
		}
		if snap.Name() == needle {
			return snap
		}
	}

	return nil
}

// FindSnapsByName returns all snaps with the given name in the "haystack"
// slice of snaps (useful for filtering)
func FindSnapsByName(needle string, haystack []*Snap) (res []*Snap) {
	name, developer := SplitDeveloper(needle)
	ignorens := developer == ""

	for _, snap := range haystack {
		if snap.Name() == name && (ignorens || snap.Developer() == developer) {
			res = append(res, snap)
		}
	}

	return res
}

// SplitDeveloper splits a snappy name name into a (name, developer) pair
func SplitDeveloper(name string) (string, string) {
	idx := strings.LastIndexAny(name, ".")
	if idx > -1 {
		return name[:idx], name[idx+1:]
	}

	return name, ""
}

// FindSnapsByNameAndVersion returns the snaps with the name/version in the
// given slice of snaps
func FindSnapsByNameAndVersion(needle, version string, haystack []*Snap) []*Snap {
	name, developer := SplitDeveloper(needle)
	ignorens := developer == ""
	var found []*Snap

	for _, snap := range haystack {
		if snap.Name() == name && snap.Version() == version && (ignorens || snap.Developer() == developer) {
			found = append(found, snap)
		}
	}

	return found
}

// MakeSnapActiveByNameAndVersion makes the given snap version the active
// version
func makeSnapActiveByNameAndVersion(pkg, ver string, inter progress.Meter) error {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return err
	}

	overlord := &Overlord{}
	snaps := FindSnapsByNameAndVersion(pkg, ver, installed)
	switch len(snaps) {
	case 0:
		return fmt.Errorf("Can not find %s with version %s", pkg, ver)
	case 1:
		return overlord.SetActive(snaps[0], true, inter)
	default:
		return fmt.Errorf("More than one %s with version %s", pkg, ver)
	}
}

// PackageNameActive checks whether a fork of the given name is active in the system
func PackageNameActive(name string) bool {
	return ActiveSnapByName(name) != nil
}

// RemoteManifestPath returns the would be path for the store manifest meta data
func RemoteManifestPath(s *snap.Info) string {
	return filepath.Join(dirs.SnapMetaDir, fmt.Sprintf("%s_%s.manifest", QualifiedName(s), s.Version))
}
