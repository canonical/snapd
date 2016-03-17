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

// QualifiedName of a Part is the Name, in most cases qualified with the
// Developer
func QualifiedName(p Part) string {
	if t := p.Type(); t == snap.TypeFramework || t == snap.TypeGadget {
		return p.Name()
	}
	return p.Name() + "." + p.Developer()
}

// BareName of a Part is just its Name
func BareName(p Part) string {
	return p.Name()
}

// FullName of a Part is Name.Developer
func FullName(p Part) string {
	return p.Name() + "." + p.Developer()
}

// FullNameWithChannel returns the FullName, with the channel appended
// if it has one.
func fullNameWithChannel(p Part) string {
	name := FullName(p)
	ch := p.Channel()
	if ch == "" {
		return name
	}

	return fmt.Sprintf("%s/%s", name, ch)
}

// Part representation of a snappy part
type Part interface {
	Name() string
	Version() string
	Developer() string
	Channel() string
	Type() snap.Type
}

// ActiveSnapsByType returns all installed snaps with the given type
func ActiveSnapsByType(snapTs ...snap.Type) (res []*Snap, err error) {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return nil, err
	}

	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		for i := range snapTs {
			if part.Type() == snapTs[i] {
				res = append(res, part)
			}
		}
	}

	return res, nil
}

// ActiveSnapIterByType returns the result of applying the given
// function to all active snaps with the given type.
var ActiveSnapIterByType = activeSnapIterByTypeImpl

func activeSnapIterByTypeImpl(f func(Part) string, snapTs ...snap.Type) ([]string, error) {
	installed, err := ActiveSnapsByType(snapTs...)
	res := make([]string, len(installed))

	for i, part := range installed {
		res[i] = f(part)
	}

	return res, err
}

// ActiveSnapByName returns all active snaps with the given name
func ActiveSnapByName(needle string) *Snap {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return nil
	}
	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		if part.Name() == needle {
			return part
		}
	}

	return nil
}

// FindSnapsByName returns all snaps with the given name in the "haystack"
// slice of parts (useful for filtering)
func FindSnapsByName(needle string, haystack []*Snap) (res []*Snap) {
	name, developer := SplitDeveloper(needle)
	ignorens := developer == ""

	for _, part := range haystack {
		if part.Name() == name && (ignorens || part.Developer() == developer) {
			res = append(res, part)
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

// FindSnapsByNameAndVersion returns the parts with the name/version in the
// given slice of parts
func FindSnapsByNameAndVersion(needle, version string, haystack []*Snap) []*Snap {
	name, developer := SplitDeveloper(needle)
	ignorens := developer == ""
	var found []*Snap

	for _, part := range haystack {
		if part.Name() == name && part.Version() == version && (ignorens || part.Developer() == developer) {
			found = append(found, part)
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
	parts := FindSnapsByNameAndVersion(pkg, ver, installed)
	switch len(parts) {
	case 0:
		return fmt.Errorf("Can not find %s with version %s", pkg, ver)
	case 1:
		return overlord.SetActive(parts[0], true, inter)
	default:
		return fmt.Errorf("More than one %s with version %s", pkg, ver)
	}
}

// PackageNameActive checks whether a fork of the given name is active in the system
func PackageNameActive(name string) bool {
	return ActiveSnapByName(name) != nil
}

// RemoteManifestPath returns the would be path for the store manifest meta data
func RemoteManifestPath(s Part) string {
	return filepath.Join(dirs.SnapMetaDir, fmt.Sprintf("%s_%s.manifest", QualifiedName(s), s.Version()))
}
