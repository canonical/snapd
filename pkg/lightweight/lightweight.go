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

// Package lightweight provides a quick way of loading things that can become snaps.
//
// A lightweight.PartBag has a name and n versions; it might not even know its origin.
package lightweight

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/removed"
	"github.com/ubuntu-core/snappy/snappy"
)

// split a path into the name and extension of the directory, and the file.
// e.g. foo/bar.baz/quux -> bar, baz, quux
//
// panics if given path is lacking at least one separator (ie bar/quux
// works (barely); quux panics). As it's supposed to be called with
// the results of a glob on <pkgdir>/<pkg|*>/*, this is only a problem
// if it's being used wrong.
func split(path string) (name string, ext string, file string) {
	const sep = string(os.PathSeparator)
	idxFileSep := strings.LastIndex(path, sep)
	if idxFileSep < 0 {
		panic("bad path given to split: must have at least two separators")
	}

	file = path[idxFileSep+1:]
	path = path[:idxFileSep]
	name = path

	idxDirSep := strings.LastIndex(path, sep)
	if idxDirSep > -1 {
		name = path[idxDirSep+1:]
	}

	idxOrig := strings.LastIndex(name, ".")
	if idxOrig < 0 {
		return name, "", file
	}

	return name[:idxOrig], name[idxOrig+1:], file
}

// extract the name, origin and list of versions from a list of paths that
// end {name}[.{origin}]/{version}. If the name or origin changes, stop and
// return the versions so far, and the remaining paths.
//
// Calls split() on each path in paths, so will panic if it does not have
// the right number of path separators, as it's a programming error.
func extract(paths []string) (string, string, []string, []string) {
	name, origin, _ := split(paths[0])

	var versions []string
	for len(paths) > 0 {
		n, o, v := split(paths[0])
		if name != n || origin != o {
			break
		}
		paths = paths[1:]

		if v == "current" {
			continue
		}

		versions = append(versions, v)
	}

	return name, origin, versions, paths
}

func versionSort(versions []string) {
	sort.Sort(sort.Reverse(snappy.ByVersion(versions)))
}

// PartBagByName finds a PartBag with the given name.
func PartBagByName(name string, origin string) *PartBag {
	if strings.ContainsAny(name, ".*?/") || strings.ContainsAny(origin, ".*?/") {
		panic("invalid name " + name + "." + origin)
	}

	for _, v := range find(name, origin) {
		return v
	}

	return nil
}

// AllPartBags the PartBags in the system.
func AllPartBags() map[string]*PartBag {
	return find("*", "*")
}

type repo interface {
	All() ([]snappy.Part, error)
}

func newCoreRepoImpl() repo {
	return snappy.NewSystemImageRepository()
}

var newCoreRepo = newCoreRepoImpl

func find(name string, origin string) map[string]*PartBag {
	bags := make(map[string]*PartBag)

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

			bag := &PartBag{
				Name:     snappy.SystemImagePartName,
				Origin:   snappy.SystemImagePartOrigin,
				Type:     pkg.TypeCore,
				Versions: versions,
				concrete: &concreteCore{},
			}
			bags[bag.QualifiedName()] = bag
		}
	}

	type T struct {
		inst string
		qn   string
		typ  pkg.Type
	}

	for _, s := range []T{
		{dirs.SnapAppsDir, name + "." + origin, pkg.TypeApp},
		{dirs.SnapAppsDir, name, pkg.TypeFramework}, // frameworks are installed under /apps also, for now
	} {
		// all snaps share the data dir, hence this bit of mess
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

			typ := s.typ
			inst := s.inst

			// if oems were removable, there'd be no way of
			// telling the kind of a removed origin-less package
			//
			// in case it's not clear, we're walking the *data*
			// directory, where directories for packages of all
			// types are present. OEM packages and frameworks look
			// the same in the data dir: they both have no
			// origin. However, OEM packages are uninstallable, and
			// you can't install a package with the same name as an
			// active package, so if /oem/{name} exists, we switch
			// this package to be type OEM.
			//
			// Right now you could, in theory, *deactivate* an oem
			// package, and install a framework with the same name
			// as the oem package you deactivated. You get to keep
			// the parts.
			if typ == pkg.TypeFramework && helpers.FileExists(filepath.Join(dirs.SnapOemDir, name)) {
				typ = pkg.TypeOem
				inst = dirs.SnapOemDir
			}

			bag := &PartBag{
				Name:     name,
				Origin:   origin,
				Type:     typ,
				Versions: versions,
			}

			bag.concrete = NewConcrete(bag, inst)

			bags[bag.QualifiedName()] = bag
		}
	}

	return bags
}

// A PartBag is a lightweight object that represents and knows how to
// load a Part on demand.
type PartBag struct {
	Name     string
	Origin   string
	Type     pkg.Type
	Versions []string
	concrete Concreter
}

// Concreter hides the part-specific details of PartBags
type Concreter interface {
	IsInstalled(string) bool
	ActiveIndex() int
	Load(string) (snappy.Part, error)
}

// NewConcrete is meant to be overridden in tests; is called when
// needing a Concreter for app/fmk/oem snaps (ie not core).
var NewConcrete = newConcreteImpl

func newConcreteImpl(bag *PartBag, instdir string) Concreter {
	return &concreteSnap{
		self:    bag,
		instdir: instdir,
	}
}

// QualifiedName of the PartBag.
//
// because PartBags read their origin from the filesystem, you don't need
// to check the pacakge type.
func (bag *PartBag) QualifiedName() string {
	if bag.Origin == "" {
		return bag.Name
	}
	return bag.FullName()
}

// FullName of the PartBag
func (bag *PartBag) FullName() string {
	return bag.Name + "." + bag.Origin
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
	self    *PartBag
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
	// If it ever becomes a problem, remember bag.Versions is sorted
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
func (bag *PartBag) IsInstalled(idx int) bool {
	if idx < 0 || idx >= len(bag.Versions) {
		return false
	}

	return bag.concrete.IsInstalled(bag.Versions[idx])
}

// ActiveIndex returns the index of the active version, or -1
func (bag *PartBag) ActiveIndex() int {
	if bag == nil || len(bag.Versions) == 0 {
		return -1
	}

	return bag.concrete.ActiveIndex()
}

// Load a Part from the PartBag
func (bag *PartBag) Load(versionIdx int) (snappy.Part, error) {
	if bag == nil {
		return nil, nil
	}

	if versionIdx < 0 || versionIdx >= len(bag.Versions) {
		return nil, ErrBadVersionIndex
	}

	version := bag.Versions[versionIdx]

	return bag.concrete.Load(version)
}

// LoadActive gets the active index and loads it.
// If none active, returns a nil Part and ErrBadVersionIndex.
func (bag *PartBag) LoadActive() (snappy.Part, error) {
	return bag.Load(bag.ActiveIndex())
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
func (bag *PartBag) LoadBest() snappy.Part {
	if bag == nil {
		return nil
	}
	if len(bag.Versions) == 0 {
		return nil
	}

	activeIdx := bag.ActiveIndex()
	if part, err := bag.Load(activeIdx); err == nil {
		return part
	}

	for i := 0; i < len(bag.Versions); i++ {
		if bag.IsInstalled(i) {
			if part, err := bag.Load(i); err == nil {
				return part
			}
		}
	}

	part, _ := bag.Load(0)

	return part
}

// Map this PartBag into a map[string]string, augmenting it with the
// given (purportedly remote) Part.
//
// It is a programming error (->panic) to call Map on a nil *PartBag with
// a nil Part. PartBag or part may be nil, but not both.
//
// Also may panic if the remote part is nil and LoadBest can't load a
// Part at all.
func (bag *PartBag) Map(remotePart snappy.Part) map[string]string {
	var version, update, rollback, icon, name, origin, _type, description string

	if bag == nil && remotePart == nil {
		panic("part bag & part both nil -- how did i even get here")
	}

	status := "not installed"
	installedSize := "-1"
	downloadSize := "-1"

	part := bag.LoadBest()
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

		downloadSize = strconv.FormatInt(remotePart.DownloadSize(), 10)
	}

	if activeIdx := bag.ActiveIndex(); activeIdx >= 0 {
		if remotePart != nil && version != remotePart.Version() {
			// XXX: this does not handle the case where the
			// one in the store is not the greatest version
			// (e.g.: store has 1.1, locally available 1.1,
			// 1.2, active 1.2)
			update = remotePart.Version()
		}

		for i := activeIdx + 1; i < len(bag.Versions); i++ {
			// XXX: it's also possible to "roll back" to a
			// store version in the case mentioned above;
			// also not covered by this code.
			if bag.IsInstalled(i) {
				rollback = bag.Versions[i]
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
		"vendor":         "",
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
