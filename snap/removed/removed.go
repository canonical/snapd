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

// Package removed implements Removed packages, that are packages that
// have been installed, removed, but not purged: there is no
// application, but there might be data.
package removed

import (
	"errors"
	"io/ioutil"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/remote"
	"github.com/ubuntu-core/snappy/snappy"
)

// ErrRemoved is returned when you ask to operate on a removed package.
var ErrRemoved = errors.New("package is removed")

// New returns the snap.Info for a removed package.
func New(name, developer, version string, pkgType snap.Type) *snap.Info {
	info := &snap.Info{
		Name:      name,
		Developer: developer,
		Version:   version,
		Type:      pkgType,
	}

	// try to load the remote manifest, that would've been kept
	// around when installing from the store.
	var remote *remote.Snap
	content, _ := ioutil.ReadFile(snappy.RemoteManifestPath(info))
	if err := yaml.Unmarshal(content, &remote); err == nil && remote != nil {
		info.Description = remote.Description
		info.Developer = remote.Developer
	}

	return info
}
