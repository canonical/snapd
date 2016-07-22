// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"path/filepath"

	"gopkg.in/yaml.v2"
)

type gadgetYaml struct {
	Bootloader string `yaml:"bootloader,omitempty"`
}

type GadgetInfo struct {
	Bootloader string
}

func ReadGadgetInfo(info *Info) (*GadgetInfo, error) {
	const errorFormat = "cannot read gadget snap details: %s"

	gadgetYamlFn := filepath.Join(info.MountDir(), "meta", "gadget.yaml")
	kmeta, err := ioutil.ReadFile(gadgetYamlFn)
	if err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	var gy gadgetYaml
	if err := yaml.Unmarshal(kmeta, &gy); err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	// basic validation
	if gy.Bootloader == "" {
		return nil, fmt.Errorf(errorFormat, "missing bootloader in gadget.yaml")
	}

	return &GadgetInfo{Bootloader: gy.Bootloader}, nil
}
