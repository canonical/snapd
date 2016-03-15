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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap"
)

// SnapFile is a local snap file that can get installed
type SnapFile struct {
	m   *snapYaml
	deb snap.File

	developer string
	instdir   string
}

// NewSnapFile loads a snap from the given snapFile
func NewSnapFile(snapFile string, developer string, unsignedOk bool) (*SnapFile, error) {
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
	if developer == SideloadedDeveloper {
		m.Version = newSideloadVersion()
	}

	fullName := m.qualifiedName(developer)
	instDir := filepath.Join(targetDir, fullName, m.Version)

	return &SnapFile{
		instdir:   instDir,
		developer: developer,
		m:         m,
		deb:       d,
	}, nil
}

// Type returns the type of the Snap (app, gadget, ...)
func (s *SnapFile) Type() snap.Type {
	if s.m.Type != "" {
		return s.m.Type
	}

	// if not declared its a app
	return "app"
}

// Info returns the snap.Info data.
func (s *SnapFile) Info() *snap.Info {
	if info, err := s.deb.Info(); err == nil {
		// developer is something that no
		info.Developer = s.developer
		return info
	}
	return nil
}

// Name returns the name
func (s *SnapFile) Name() string {
	return s.m.Name
}

// Version returns the version
func (s *SnapFile) Version() string {
	return s.m.Version
}

// Developer returns the developer
func (s *SnapFile) Developer() string {
	return s.developer
}
