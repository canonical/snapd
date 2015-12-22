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

package info

import (
	"fmt"
	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/pkg"
)

type packageYaml struct {
	Name    string
	Version string
	Type    pkg.Type
}

// SnapInfoPackageYaml implements the meta/snap.yaml data
type SnapInfoPackageYaml struct {
	m packageYaml
}

// NewFromPackageYaml creates a new info based on the given packageYaml
func NewFromPackageYaml(yamlData []byte) (Info, error) {
	var s SnapInfoPackageYaml
	err := yaml.Unmarshal(yamlData, &s.m)
	if err != nil {
		return nil, fmt.Errorf("info failed to parse: %s", err)
	}

	// FIXME: validation of the fields

	return &s, nil
}

// Name returns the name of the snap
func (s *SnapInfoPackageYaml) Name() string {
	return s.m.Name
}

// Version returns the version of the snap
func (s *SnapInfoPackageYaml) Version() string {
	return s.m.Version
}

// Type returns the type of the snap
func (s *SnapInfoPackageYaml) Type() pkg.Type {
	return s.m.Type
}
