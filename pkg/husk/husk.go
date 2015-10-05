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

// Package husk provides a quick way of loading things that can become snaps.
//
// A husk has a name and n versions; it might not even know its origin.
package husk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/pkg/removed"
	"launchpad.net/snappy/snappy"
)

// split a path into the name and extension of the directory, and the file.
// e.g. foo/bar.baz/quux -> bar, baz, quux
//
// panics if given path is lacking at least one separator (ie bar/quux
// works (barely); quux panics)
func split(path string) (name string, ext string, file string) {
	idxFileSep := strings.LastIndexByte(path, os.PathSeparator)
	if idxFileSep < 0 {
		panic("bad path given to split: must have at least two separators")
	}

	file = path[idxFileSep+1:]
	path = path[:idxFileSep]
	name = path

	idxDirSep := strings.LastIndexByte(path, os.PathSeparator)
	if idxDirSep > -1 {
		name = path[idxDirSep+1:]
	}

	idxOrig := strings.LastIndexByte(name, '.')
	if idxOrig < 0 {
		return name, "", file
	}

	return name[:idxOrig], name[idxOrig+1:], file
}

// extract the name, origin and list of versions from a list of paths that
// end {name}[.{origin}]/{version}. If the origin changes, stop and
// return the versions so far, and the remaining paths.
func extract(paths []string) (string, string, []string, []string) {
	name, origin, _ := split(paths[0])

	var versions []string
	for len(paths) > 0 {
		n, o, v := split(paths[0])
		if name != n || origin != o {
			break
		}

		versions = append(versions, v)
		paths = paths[1:]
	}

	return name, origin, versions, paths
}

func versionSort(versions []string) {
	sort.Sort(sort.Reverse(snappy.ByVersion(versions)))
}

// ByName finds husks with the given name.
func ByName(name string, origin string) *Husk {
	if strings.ContainsAny(name, ".*?/") || strings.ContainsAny(origin, ".*?/") {
		panic("invalid name " + name + "." + origin)
	}

	for _, v := range find(name, origin) {
		return v
	}

	return nil
}

// All the husks in the system.
func All() map[string]*Husk {
	return find("*", "*")
}

type repo interface {
	All() ([]snappy.Part, error)
}

func newCoreRepoImpl() repo {
	return snappy.NewSystemImageRepository()
}

var newCoreRepo = newCoreRepoImpl

func find(name string, origin string) map[string]*Husk {
	husks := make(map[string]*Husk)

	if (name == snappy.SystemImagePartName || name == "*") && (origin == snappy.SystemImagePartOrigin || origin == "*") {
		// TODO: make this do less work
		repo := newCoreRepo()
		parts, err := repo.All()
		if err != nil {
			//  can't really happen
			panic(fmt.Sprintf("Bad SystemImageRepository: %v", err))
		}

		// parts can be empty during testing for example
		if len(parts) > 0 {
			versions := make([]string, len(parts))
			for i, part := range parts {
				versions[i] = part.Version()
			}
			versionSort(versions)

			husk := &Husk{
				Name:     snappy.SystemImagePartName,
				Origin:   snappy.SystemImagePartOrigin,
				Type:     pkg.TypeCore,
				Versions: versions,
				concrete: &concreteCore{},
			}
			husks[husk.QualifiedName()] = husk
		}
	}

	type T struct {
		inst string
		qn   string
		typ  pkg.Type
	}

	for _, s := range []T{
		{dirs.SnapAppsDir, name + "." + origin, pkg.TypeApp},
		{dirs.SnapAppsDir, name, pkg.TypeFramework},
	} {
		paths, _ := filepath.Glob(filepath.Join(dirs.SnapDataDir, s.qn, "*"))
		for len(paths) > 0 {
			var name string
			var origin string
			var versions []string

			name, origin, versions, paths = extract(paths)
			if origin != "" && s.typ != pkg.TypeApp {
				// this happens when called with name="*"
				continue
			}

			versionSort(versions)

			if s.typ == pkg.TypeFramework && helpers.FileExists(filepath.Join(dirs.SnapOemDir, name)) {
				s.typ = pkg.TypeOem
				s.inst = dirs.SnapOemDir
			}

			husk := &Husk{
				Name:     name,
				Origin:   origin,
				Type:     s.typ,
				Versions: versions,
			}

			husk.concrete = NewConcrete(husk, s.inst)

			husks[husk.QualifiedName()] = husk
		}
	}

	return husks
}

// A Husk is a lightweight object that represents and knows how to
// load a Part on demand.
type Husk struct {
	Name     string
	Origin   string
	Type     pkg.Type
	Versions []string
	concrete Concreter
}

// Concreter hides the part-specific details of husks
type Concreter interface {
	IsInstalled(string) bool
	ActiveIndex() int
	Load(string) (snappy.Part, error)
}

// NewConcrete is meant to be overridden in tests; is called when
// needing a Concreter for app/fmk/oem snaps (ie not core).
var NewConcrete = newConcreteImpl

func newConcreteImpl(husk *Husk, instdir string) Concreter {
	return &concreteSnap{
		self:    husk,
		instdir: instdir,
	}
}

// QualifiedName of the husk.
//
// because husks read their origin from the filesystem, you don't need
// to check the pacakge type.
func (h *Husk) QualifiedName() string {
	if h.Origin == "" {
		return h.Name
	}
	return h.FullName()
}

// FullName of the husk
func (h *Husk) FullName() string {
	return h.Name + "." + h.Origin
}

var (
	// ErrBadVersionIndex is returned by Load when asked to load a
	// non-existent version.
	ErrBadVersionIndex = errors.New("Bad version index")
	// ErrVersionGone is returned in the case where we find a
	// version and it disappears before we get to load it.
	ErrVersionGone = errors.New("Version gone")
)

type concreteCore struct{}

func (*concreteCore) IsInstalled(string) bool { return true }
func (*concreteCore) ActiveIndex() int        { return 0 }
func (*concreteCore) Load(version string) (snappy.Part, error) {
	parts, err := newCoreRepo().All()
	if err != nil {
		//  can't really happen
		return nil, fmt.Errorf("Bad SystemImageRepository: %v", err)
	}

	for _, part := range parts {
		if part.Version() == version {
			return part, nil
		}
	}

	return nil, ErrVersionGone
}

type concreteSnap struct {
	self    *Husk
	instdir string
}

func (c *concreteSnap) IsInstalled(version string) bool {
	return helpers.FileExists(filepath.Join(c.instdir, c.self.QualifiedName(), version, "meta", "package.yaml"))
}

func (c *concreteSnap) ActiveIndex() int {
	current, err := os.Readlink(filepath.Join(c.instdir, c.self.QualifiedName(), "current"))
	if err != nil {
		return -1
	}

	current = filepath.Base(current)

	// Linear search is fine for now.
	//
	// If it ever becomes a problem, remember h.Versions is sorted
	// so you can use go's sort.Search and snappy.VersionCompare,
	// but VersionCompare is not cheap, so that (on my machine, at
	// the time of writing) linear of even 100k versions is only
	// 2×-3× slower than binary; anything below about 50k versions
	// is faster even in worst-case for linear (no match). And
	// that's not even looking at memory impact.
	//
	// For example, on the ~90k lines in /usr/share/dict/words:
	//
	// BenchmarkBinaryPositive-4	   10000	    135041 ns/op	   11534 B/op	     316 allocs/op
	// BenchmarkBinaryNegative-4	   10000	    109803 ns/op	   10235 B/op	     231 allocs/op
	// BenchmarkLinearPositive-4	    5000	    272980 ns/op	       0 B/op	       0 allocs/op
	// BenchmarkLinearNegative-4	    5000	    362244 ns/op	       0 B/op	       0 allocs/op
	//
	// on a corpus of 100k %016x-formatted rand.Int63()s:
	//
	// BenchmarkBinaryNegative-4	   10000	    152797 ns/op	   10158 B/op	     243 allocs/op
	// BenchmarkLinearNegative-4	   10000	    163924 ns/op	       0 B/op	       0 allocs/op
	//
	// ... I think I might need help.
	for i := range c.self.Versions {
		if c.self.Versions[i] == current {
			return i
		}
	}

	return -1
}

func (c *concreteSnap) Load(version string) (snappy.Part, error) {
	yamlPath := filepath.Join(c.instdir, c.self.QualifiedName(), version, "meta", "package.yaml")
	if !helpers.FileExists(yamlPath) {
		return removed.New(c.self.Name, c.self.Origin, version, c.self.Type), nil
	}

	part, err := snappy.NewInstalledSnapPart(yamlPath, c.self.Origin)
	if err != nil {
		return nil, err
	}

	return part, nil
}

// IsInstalled checks whether the given part is installed
func (h *Husk) IsInstalled(idx int) bool {
	if idx < 0 || idx >= len(h.Versions) {
		return false
	}

	return h.concrete.IsInstalled(h.Versions[idx])
}

// ActiveIndex returns the index of the active version, or -1
func (h *Husk) ActiveIndex() int {
	if h == nil || len(h.Versions) == 0 {
		return -1
	}

	return h.concrete.ActiveIndex()
}

// Load a Part from the Husk
func (h *Husk) Load(versionIdx int) (snappy.Part, error) {
	if h == nil {
		return nil, nil
	}

	if versionIdx < 0 || versionIdx >= len(h.Versions) {
		return nil, ErrBadVersionIndex
	}

	version := h.Versions[versionIdx]

	return h.concrete.Load(version)
}

// LoadBest looks for the best candidate Part and loads it.
//
// If there is an active part, load that. Otherwise, load the
// highest-versioned installed part. Otherwise, load the first removed
// part.
//
// If not even a removed part can be loaded, something is wrong. Nil
// is returned, but you're in trouble (did the filesystem just
// disappear under us?).
func (h *Husk) LoadBest() snappy.Part {
	if h == nil {
		return nil
	}
	if len(h.Versions) == 0 {
		return nil
	}

	activeIdx := h.ActiveIndex()
	if part, err := h.Load(activeIdx); err == nil {
		return part
	}

	for i := 0; i < len(h.Versions); i++ {
		if h.IsInstalled(i) {
			if part, err := h.Load(i); err == nil {
				return part
			}
		}
	}

	part, _ := h.Load(0)

	return part
}

// Map this husk into a map[string]string, augmenting it with the
// given (purportedly remote) Part.
//
// It is a programming error (->panic) to call Map on a nil *Husk with
// a nil Part. Husk or part may be nil, but not both.
//
// Also may panic if the remote part is nil and LoadBest can't load a
// Part at all.
func (h *Husk) Map(remotePart snappy.Part) map[string]string {
	var version, update, rollback, icon, name, origin, _type, vendor, description string

	if h == nil && remotePart == nil {
		panic("husk & part both nil -- how did i even get here")
	}

	status := "not installed"
	installedSize := "-1"
	downloadSize := "-1"

	part := h.LoadBest()
	if part != nil {
		if part.IsActive() {
			status = "active"
		} else if part.IsInstalled() {
			status = "installed"
		} else {
			status = "removed"
		}
	} else if remotePart == nil {
		panic("unable to load a valid part")
	}

	if part != nil {
		name = part.Name()
		origin = part.Origin()
		version = part.Version()
		_type = string(part.Type())

		icon = part.Icon()
		vendor = part.Vendor()
		description = part.Description()
		installedSize = strconv.FormatInt(part.InstalledSize(), 10)

		downloadSize = strconv.FormatInt(part.DownloadSize(), 10)
	} else {
		name = remotePart.Name()
		origin = remotePart.Origin()
		version = remotePart.Version()
		_type = string(remotePart.Type())
	}

	if remotePart != nil {
		if icon == "" {
			icon = remotePart.Icon()
		}
		if description == "" {
			description = remotePart.Description()
		}
		if vendor == "" {
			vendor = remotePart.Vendor()
		}

		downloadSize = strconv.FormatInt(remotePart.DownloadSize(), 10)
	}

	if activeIdx := h.ActiveIndex(); activeIdx >= 0 {
		if remotePart != nil && version != remotePart.Version() {
			// XXX: this does not handle the case where the
			// one in the store is not the greatest version
			// (e.g.: store has 1.1, locally available 1.1,
			// 1.2, active 1.2)
			update = remotePart.Version()
		}

		for i := activeIdx + 1; i < len(h.Versions); i++ {
			// XXX: it's also possible to "roll back" to a
			// store version in the case mentioned above;
			// also not covered by this code.
			if h.IsInstalled(i) {
				rollback = h.Versions[i]
				break
			}
		}
	}

	result := map[string]string{
		"icon":           icon,
		"name":           name,
		"origin":         origin,
		"status":         status,
		"type":           _type,
		"vendor":         vendor,
		"version":        version,
		"description":    description,
		"installed_size": installedSize,
		"download_size":  downloadSize,
	}

	if rollback != "" {
		result["rollback_available"] = rollback
	}

	if update != "" {
		result["update_available"] = update
	}

	return result
}
