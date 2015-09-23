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
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/progress"
)

// SystemConfig is a config map holding configs for multiple packages
type SystemConfig map[string]interface{}

// ServiceYamler implements snappy packages that offer services
type ServiceYamler interface {
	ServiceYamls() []ServiceYaml
}

// Configuration allows requesting an oem snappy package type's config
type Configuration interface {
	OemConfig() SystemConfig
}

// QualifiedName of a Part is the Name, in most cases qualified with the
// Origin
func QualifiedName(p Part) string {
	if t := p.Type(); t == pkg.TypeFramework || t == pkg.TypeOem {
		return p.Name()
	}
	return p.Name() + "." + p.Origin()
}

// Part representation of a snappy part
type Part interface {

	// query
	Name() string
	Version() string
	Description() string
	Origin() string
	Vendor() string

	Hash() string
	IsActive() bool
	IsInstalled() bool
	// Will become active on the next reboot
	NeedsReboot() bool

	// returns the date when the snap was last updated
	Date() time.Time

	// returns the channel of the part
	Channel() string

	// returns the path to the icon (local or uri)
	Icon() string

	// Returns app, framework, core
	Type() pkg.Type

	InstalledSize() int64
	DownloadSize() int64

	// Install the snap
	Install(pb progress.Meter, flags InstallFlags) (name string, err error)
	// Uninstall the snap
	Uninstall(pb progress.Meter) error
	// Config takes a yaml configuration and returns the full snap
	// config with the changes. Note that "configuration" may be empty.
	Config(configuration []byte) (newConfig string, err error)
	// make a inactive part active
	SetActive(pb progress.Meter) error

	// get the list of frameworks needed by the part
	Frameworks() ([]string, error)
}

// Repository is the interface for a collection of snaps
type Repository interface {

	// query
	Description() string

	// action
	Details(name string, origin string) ([]Part, error)

	Updates() ([]Part, error)
	Installed() ([]Part, error)

	All() ([]Part, error)
}

// MetaRepository contains all available single repositories can can be used
// to query in a single place
type MetaRepository struct {
	all []Repository
}

// NewMetaStoreRepository returns a MetaRepository of stores
func NewMetaStoreRepository() *MetaRepository {
	m := new(MetaRepository)
	m.all = []Repository{}

	if repo := NewUbuntuStoreSnapRepository(); repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

// NewMetaLocalRepository returns a MetaRepository of stores
func NewMetaLocalRepository() *MetaRepository {
	m := new(MetaRepository)
	m.all = []Repository{}

	if repo := NewSystemImageRepository(); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewLocalSnapRepository(snapAppsDir); repo != nil {
		m.all = append(m.all, repo)
	}
	if repo := NewLocalSnapRepository(snapOemDir); repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

// NewMetaRepository returns a new MetaRepository
func NewMetaRepository() *MetaRepository {
	// FIXME: make this a configuration file

	m := NewMetaLocalRepository()
	if repo := NewUbuntuStoreSnapRepository(); repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

// Installed returns all installed parts
func (m *MetaRepository) Installed() (parts []Part, err error) {
	for _, r := range m.all {
		installed, err := r.Installed()
		if err != nil {
			return parts, err
		}
		parts = append(parts, installed...)
	}

	return parts, err
}

// All the parts
func (m *MetaRepository) All() ([]Part, error) {
	var parts []Part

	for _, r := range m.all {
		all, err := r.All()
		if err != nil {
			return nil, err
		}
		parts = append(parts, all...)
	}

	return parts, nil
}

// Updates returns all updatable parts
func (m *MetaRepository) Updates() (parts []Part, err error) {
	for _, r := range m.all {
		updates, err := r.Updates()
		if err != nil {
			return parts, err
		}
		parts = append(parts, updates...)
	}

	return parts, err
}

// Details returns details for the given snap name
func (m *MetaRepository) Details(name string, origin string) ([]Part, error) {
	var parts []Part

	for _, r := range m.all {
		results, err := r.Details(name, origin)
		// ignore network errors here, we will also collect
		// local results
		_, netError := err.(net.Error)
		_, urlError := err.(*url.Error)
		switch {
		case err == ErrPackageNotFound || netError || urlError:
			continue
		case err != nil:
			return nil, err
		}
		parts = append(parts, results...)
	}

	return parts, nil
}

// ActiveSnapsByType returns all installed snaps with the given type
func ActiveSnapsByType(snapTs ...pkg.Type) (res []Part, err error) {
	m := NewMetaRepository()
	installed, err := m.Installed()
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

// ActiveSnapNamesByType returns all installed snap names with the given type
var ActiveSnapNamesByType = activeSnapNamesByTypeImpl

func activeSnapNamesByTypeImpl(snapTs ...pkg.Type) ([]string, error) {
	installed, err := ActiveSnapsByType(snapTs...)
	res := make([]string, len(installed))

	for i, part := range installed {
		res[i] = QualifiedName(part)
	}

	return res, err
}

// ActiveSnapByName returns all active snaps with the given name
func ActiveSnapByName(needle string) Part {
	m := NewMetaRepository()
	installed, err := m.Installed()
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
func FindSnapsByName(needle string, haystack []Part) (res []Part) {
	name, origin := splitOrigin(needle)
	ignorens := origin == ""

	for _, part := range haystack {
		if part.Name() == name && (ignorens || part.Origin() == origin) {
			res = append(res, part)
		}
	}

	return res
}

func splitOrigin(name string) (string, string) {
	idx := strings.LastIndexAny(name, ".")
	if idx > -1 {
		return name[:idx], name[idx+1:]
	}

	return name, ""
}

// FindSnapsByNameAndVersion returns the parts with the name/version in the
// given slice of parts
func FindSnapsByNameAndVersion(needle, version string, haystack []Part) []Part {
	name, origin := splitOrigin(needle)
	ignorens := origin == ""
	var found []Part

	for _, part := range haystack {
		if part.Name() == name && part.Version() == version && (ignorens || part.Origin() == origin) {
			found = append(found, part)
		}
	}

	return found
}

// MakeSnapActiveByNameAndVersion makes the given snap version the active
// version
func makeSnapActiveByNameAndVersion(pkg, ver string, inter progress.Meter) error {
	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	parts := FindSnapsByNameAndVersion(pkg, ver, installed)
	switch len(parts) {
	case 0:
		return fmt.Errorf("Can not find %s with version %s", pkg, ver)
	case 1:
		return parts[0].SetActive(inter)
	default:
		return fmt.Errorf("More than one %s with version %s", pkg, ver)
	}
}

// PackageNameActive checks whether a fork of the given name is active in the system
func PackageNameActive(name string) bool {
	return ActiveSnapByName(name) != nil
}

// iconPath returns the would be path for the local icon
func iconPath(s Part) string {
	// TODO: care about extension ever being different than png
	return filepath.Join(snapIconsDir, fmt.Sprintf("%s_%s.png", QualifiedName(s), s.Version()))
}

// manifestPath returns the would be path for the store manifest meta data
func manifestPath(s Part) string {
	return filepath.Join(snapMetaDir, fmt.Sprintf("%s_%s.manifest", QualifiedName(s), s.Version()))
}
