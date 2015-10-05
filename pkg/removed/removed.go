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
	"time"

	"gopkg.in/yaml.v2"

	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/pkg/remote"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

// ErrRemoved is returned when you ask to operate on a removed package.
var ErrRemoved = errors.New("package is removed")

// Removed represents a removed package.
type Removed struct {
	name    string
	origin  string
	version string
	pkgType pkg.Type
	remote  *remote.Snap
}

// New removed package.
func New(name, origin, version string, pkgType pkg.Type) snappy.Part {
	part := &Removed{
		name:    name,
		origin:  origin,
		version: version,
		pkgType: pkgType,
	}

	content, _ := ioutil.ReadFile(snappy.ManifestPath(part))
	yaml.Unmarshal(content, &(part.remote))

	return part
}

// Name from the snappy.Part interface
func (r *Removed) Name() string { return r.name }

// Version from the snappy.Part interface
func (r *Removed) Version() string { return r.version }

// Description from the snappy.Part interface
func (r *Removed) Description() string {
	if r.remote != nil {
		return r.remote.Description
	}

	return ""
}

// Origin from the snappy.Part interface
func (r *Removed) Origin() string {
	if r.remote != nil {
		return r.remote.Origin
	}
	if r.origin == "" {
		return snappy.SideloadedOrigin
	}
	return r.origin
}

// Vendor from the snappy.Part interface
func (r *Removed) Vendor() string {
	if r.remote != nil {
		return r.remote.Publisher
	}

	return ""
}

// Hash from the snappy.Part interface
func (r *Removed) Hash() string { return "" }

// IsActive from the snappy.Part interface
func (r *Removed) IsActive() bool { return false }

// IsInstalled from the snappy.Part interface
func (r *Removed) IsInstalled() bool { return false }

// NeedsReboot from the snappy.Part interface
func (r *Removed) NeedsReboot() bool { return false }

// Date from the snappy.Part interface
func (r *Removed) Date() time.Time { return time.Time{} } // XXX: keep track of when the package was removed
// Channel from the snappy.Part interface
func (r *Removed) Channel() string { return "" }

// Icon from the snappy.Part interface
func (r *Removed) Icon() string {
	if r.remote != nil {
		return r.remote.IconURL
	}

	return ""
}

// Type from the snappy.Part interface
func (r *Removed) Type() pkg.Type { return r.pkgType }

// InstalledSize from the snappy.Part interface
func (r *Removed) InstalledSize() int64 { return -1 }

// DownloadSize from the snappy.Part interface
func (r *Removed) DownloadSize() int64 {
	if r.remote != nil {
		return r.remote.DownloadSize
	}

	return -1
}

// Install from the snappy.Part interface
func (r *Removed) Install(pb progress.Meter, flags snappy.InstallFlags) (name string, err error) {
	return "", ErrRemoved
}

// Uninstall from the snappy.Part interface
func (r *Removed) Uninstall(pb progress.Meter) error { return ErrRemoved }

// Config from the snappy.Part interface
func (r *Removed) Config(configuration []byte) (newConfig string, err error) { return "", ErrRemoved }

// SetActive from the snappy.Part interface
func (r *Removed) SetActive(bool, progress.Meter) error { return ErrRemoved }

// Frameworks from the snappy.Part interface
func (r *Removed) Frameworks() ([]string, error) { return nil, ErrRemoved }
