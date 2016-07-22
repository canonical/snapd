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
	"strconv"

	"gopkg.in/yaml.v2"
)

type volume map[string]string

type gadgetYaml struct {
	Bootloader string              `yaml:"bootloader"`
	Volumes    map[string][]volume `yaml:"volumes,omitempty"`
}

type Volume struct {
	Name   string
	Type   string
	Data   string
	Offset int64
}

type GadgetInfo struct {
	Bootloader string
	Volumes    map[string][]Volume
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

	gi := &GadgetInfo{
		Bootloader: gy.Bootloader,
		Volumes:    make(map[string][]Volume),
	}
	for k, vl := range gy.Volumes {
		gi.Volumes[k] = make([]Volume, len(vl))
		for i, v := range vl {
			offset, err := strconv.ParseInt(v["offset"], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot parse offset %v: %s", v["offset"], err)
			}
			gi.Volumes[k][i] = Volume{
				Name:   v["name"],
				Type:   v["type"],
				Data:   v["data"],
				Offset: offset,
			}
		}
	}

	return gi, nil
}
