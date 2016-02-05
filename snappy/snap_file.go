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
	"time"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

// SnapFile is a local snap file that can get installed
type SnapFile struct {
	m   *snapYaml
	deb snap.File

	origin  string
	instdir string
}

// NewSnapFile loads a snap from the given snapFile
func NewSnapFile(snapFile string, origin string, unsignedOk bool) (*SnapFile, error) {
	d, err := snap.Open(snapFile)
	if err != nil {
		return nil, err
	}

	yamlData, err := d.MetaMember("snap.yaml")
	if err != nil {
		return nil, err
	}

	_, err = d.MetaMember("hooks/config")
	hasConfig := err == nil

	m, err := parseSnapYamlData(yamlData, hasConfig)
	if err != nil {
		return nil, err
	}

	targetDir := dirs.SnapSnapsDir
	if origin == SideloadedOrigin {
		m.Version = helpers.NewSideloadVersion()
	}

	fullName := m.qualifiedName(origin)
	instDir := filepath.Join(targetDir, fullName, m.Version)

	return &SnapFile{
		instdir: instDir,
		origin:  origin,
		m:       m,
		deb:     d,
	}, nil
}

// Type returns the type of the SnapPart (app, gadget, ...)
func (s *SnapFile) Type() snap.Type {
	if s.m.Type != "" {
		return s.m.Type
	}

	// if not declared its a app
	return "app"
}

// Name returns the name
func (s *SnapFile) Name() string {
	return s.m.Name
}

// Version returns the version
func (s *SnapFile) Version() string {
	return s.m.Version
}

// Channel returns the channel used
func (s *SnapFile) Channel() string {
	return ""
}

// Config is used to to configure the snap
func (s *SnapFile) Config(configuration []byte) (new string, err error) {
	return "", err
}

// Date returns the last update date
func (s *SnapFile) Date() time.Time {
	return time.Time{}
}

// Description returns the description of the snap
func (s *SnapFile) Description() string {
	return ""
}

// DownloadSize returns the download size
func (s *SnapFile) DownloadSize() int64 {
	return 0
}

// InstalledSize returns the installed size
func (s *SnapFile) InstalledSize() int64 {
	return 0
}

// Hash returns the hash
func (s *SnapFile) Hash() string {
	return ""
}

// Icon returns the icon
func (s *SnapFile) Icon() string {
	return ""
}

// IsActive returns whether it is active.
func (s *SnapFile) IsActive() bool {
	return false
}

// IsInstalled returns if its installed
func (s *SnapFile) IsInstalled() bool {
	return false
}

// NeedsReboot tells if the snap needs rebooting
func (s *SnapFile) NeedsReboot() bool {
	return false
}

// Origin returns the origin
func (s *SnapFile) Origin() string {
	return s.origin
}

// Frameworks returns the list of frameworks needed by the snap
func (s *SnapFile) Frameworks() ([]string, error) {
	return s.m.Frameworks, nil
}

// Install installs the snap
func (s *SnapFile) Install(inter progress.Meter, flags InstallFlags) (name string, err error) {
	return "", ErrNotImplemented
}
