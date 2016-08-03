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

type volume struct {
	Name    string   `yaml:"name"`
	Label   string   `yaml:"label"`
	Type    string   `yaml:"type"`
	Data    string   `yaml:"data"`
	Offset  int64    `yaml:"offset"`
	Content []string `yaml:"content"`
}

type gadgetYaml struct {
	Bootloader string              `yaml:"bootloader"`
	Volumes    map[string][]volume `yaml:"volumes,omitempty"`
}

type Volume struct {
	Name    string
	Label   string
	Type    string
	Data    string
	Offset  int64
	Content []string
}

type GadgetInfo struct {
	Bootloader string
	Volumes    map[string][]Volume
}

func ReadGadgetInfo(info *Info) (*GadgetInfo, error) {
	const errorFormat = "cannot read gadget snap details: %s"

	gadgetYamlFn := filepath.Join(info.MountDir(), "meta", "gadget.yaml")
	gmeta, err := ioutil.ReadFile(gadgetYamlFn)
	if err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	var gy gadgetYaml
	if err := yaml.Unmarshal(gmeta, &gy); err != nil {
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
			if v.Name == "" {
				return nil, fmt.Errorf("volume name cannot be empty")
			}

			gi.Volumes[k][i] = Volume{
				Name:    v.Name,
				Label:   v.Label,
				Type:    v.Type,
				Data:    v.Data,
				Offset:  v.Offset,
				Content: v.Content,
			}
		}
	}

	return gi, nil
}
