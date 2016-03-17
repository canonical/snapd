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
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/remote"
)

// RemoteSnap represents a snap available on the server
type RemoteSnap struct {
	// FIXME: exported for testing
	Pkg remote.Snap
}

// Type returns the type of the Snap (app, gadget, ...)
func (s *RemoteSnap) Type() snap.Type {
	return s.Pkg.Type
}

// Name returns the name
func (s *RemoteSnap) Name() string {
	return s.Pkg.Name
}

// Version returns the version
func (s *RemoteSnap) Version() string {
	return s.Pkg.Version
}

// Description returns the description
func (s *RemoteSnap) Description() string {
	return s.Pkg.Title
}

// Developer is the developer
func (s *RemoteSnap) Developer() string {
	return s.Pkg.Developer
}

// Hash returns the hash
func (s *RemoteSnap) Hash() string {
	return s.Pkg.DownloadSha512
}

// Channel returns the channel used
func (s *RemoteSnap) Channel() string {
	return s.Pkg.Channel
}

// Icon returns the icon
func (s *RemoteSnap) Icon() string {
	return s.Pkg.IconURL
}

// IsActive returns true if the snap is active
func (s *RemoteSnap) IsActive() bool {
	return false
}

// IsInstalled returns true if the snap is installed
func (s *RemoteSnap) IsInstalled() bool {
	return false
}

// InstalledSize returns the size of the installed snap
func (s *RemoteSnap) InstalledSize() int64 {
	return -1
}

// DownloadSize returns the dowload size
func (s *RemoteSnap) DownloadSize() int64 {
	return s.Pkg.DownloadSize
}

// Date returns the last update time
func (s *RemoteSnap) Date() time.Time {
	var p time.Time
	var err error

	for _, fmt := range []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.000000Z",
	} {
		p, err = time.Parse(fmt, s.Pkg.LastUpdated)
		if err == nil {
			break
		}
	}

	return p
}

func (s *RemoteSnap) saveStoreManifest() error {
	content, err := yaml.Marshal(s.Pkg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dirs.SnapMetaDir, 0755); err != nil {
		return err
	}

	// don't worry about previous contents
	return osutil.AtomicWriteFile(RemoteManifestPath(s), content, 0644, 0)
}

// Config is used to to configure the snap
func (s *RemoteSnap) Config(configuration []byte) (new string, err error) {
	return "", err
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *RemoteSnap) NeedsReboot() bool {
	return false
}

// Frameworks returns the list of frameworks needed by the snap
func (s *RemoteSnap) Frameworks() ([]string, error) {
	return nil, ErrNotImplemented
}
