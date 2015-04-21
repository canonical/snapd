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
	"strings"
)

// A SnapDataDir represents a single data directory for a version of a package
type SnapDataDir struct {
	Base      string
	Name      string
	Namespace string
	Version   string
}

// Dirname returns the filesystem directory name for this SnapDataDir
func (dd SnapDataDir) Dirname() string {
	if dd.Namespace != "" {
		return dd.Name + "." + dd.Namespace
	}
	return dd.Name
}

func data1(spec, basedir string) []SnapDataDir {
	var snaps []SnapDataDir
	var filterns bool

	verglob := "*"
	specns := "*"

	// Note that "=" is not legal in a snap name or a snap version
	idx := strings.IndexRune(spec, '=')
	if idx > -1 {
		verglob = spec[idx+1:]
		spec = spec[:idx]
	}

	nameglob := spec + "*"
	idx = strings.IndexRune(spec, '.')
	if idx > -1 {
		filterns = true
		specns = spec[idx+1:]
		spec = spec[:idx]
		nameglob = spec + "." + specns
	}

	dirs, _ := filepath.Glob(filepath.Join(basedir, nameglob, verglob))

	// “but, Chipaca”, I hear you say, “why are you doing all this all over
	// again, when you could just use .Installed() on an appropriate repo,
	// and getNamespaceFromYaml and all the other lovely tools we already
	// have written?”
	// To which I can only say: DataDirs finds all the data dirs on the
	// system, not just those of packages that are installed. If you've
	// removed a package its package.yaml is gone, its data is still there,
	// and you want us to be able to clean that up.
	for _, dir := range dirs {
		version := filepath.Base(dir)
		name := filepath.Base(filepath.Dir(dir))
		namespace := ""
		idx := strings.IndexRune(name, '.')
		if idx > -1 {
			namespace = name[idx+1:]
			name = name[:idx]
		}
		if filterns && specns != namespace {
			continue
		}
		if spec != "" && spec != name {
			continue
		}

		snaps = append(snaps, SnapDataDir{
			Base:      basedir,
			Name:      name,
			Namespace: namespace,
			Version:   version,
		})
	}

	return snaps
}

// DataDirs returns the list of all SnapDataDirs in the system.
func DataDirs(spec string) []SnapDataDir {
	return append(data1(spec, snapDataHomeGlob), data1(spec, snapDataDir)...)
}
