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

package image

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type seedYaml struct {
	Assertions []map[string]string `yaml:"assertions"`
	Snaps      []map[string]string `yaml:"snaps"`
}

type Seed struct {
	Assertions []map[string]string
	Snaps      []map[string]string
}

func ReadSeed(fn string) (*Seed, error) {
	yamlData, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot read file %q: %s", fn, err)
	}

	var s Seed
	err = yaml.Unmarshal(yamlData, &s)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal %q: %s", yamlData, s)
	}
	return &s, nil
}
