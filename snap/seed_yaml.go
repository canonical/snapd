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

type seedSnapYaml struct {
	// FIXME: eventually we only want to have "name", "channel" here
	SideInfo `yaml:",inline"`
}

type seedYaml struct {
	Snaps []seedSnapYaml `yaml:"snaps"`
}

type SeedSnap struct {
	SideInfo
}

type Seed struct {
	Snaps []*SeedSnap
}

func ReadSeedYaml(fn string) (*Seed, error) {
	yamlData, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot read seed yaml: %s", fn)
	}

	var y seedYaml
	if err := yaml.Unmarshal(yamlData, &y); err != nil {
		return nil, fmt.Errorf("cannot unmarshal %q: %s", yamlData, err)
	}

	seed := &Seed{
		Snaps: make([]*SeedSnap, len(y.Snaps)),
	}
	for i, ys := range y.Snaps {
		seed.Snaps[i] = &SeedSnap{}
		seed.Snaps[i].SideInfo = ys.SideInfo
	}
	return seed, nil
}
