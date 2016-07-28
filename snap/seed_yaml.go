// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type SeedSnap struct {
	// yaml needs to be in sync with SideInfo
	RealName    string   `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID      string   `yaml:"snap-id" json:"snap-id"`
	Revision    Revision `yaml:"revision" json:"revision"`
	Channel     string   `yaml:"channel,omitempty" json:"channel,omitempty"`
	DeveloperID string   `yaml:"developer-id,omitempty" json:"developer-id,omitempty"`
	Developer   string   `yaml:"developer,omitempty" json:"developer,omitempty"` // XXX: obsolete, will be retired after full backfilling of DeveloperID
	Private     bool     `yaml:"private,omitempty" json:"private,omitempty"`

	// not in side-info
	File string `yaml:"file"`
}

type Seed struct {
	Snaps []*SeedSnap `yaml:"snaps"`
}

func ReadSeedYaml(fn string) (*Seed, error) {
	yamlData, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot read seed yaml: %s", fn)
	}

	var seed Seed
	if err := yaml.Unmarshal(yamlData, &seed); err != nil {
		return nil, fmt.Errorf("cannot unmarshal %q: %s", yamlData, err)
	}

	return &seed, nil
}
