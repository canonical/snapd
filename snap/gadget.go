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

type volume map[string]interface{}

type gadgetYaml struct {
	Bootloader string              `yaml:"bootloader"`
	Volumes    map[string][]volume `yaml:"volumes,omitempty"`
}

type Volume struct {
	Name  string
	Label string
	Type  string
	Data  string
	// FIXME: use int64
	Offset  int
	Content []string
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
			// name
			name, ok := v["name"].(string)
			if !ok {
				return nil, fmt.Errorf(errorFormat, kmeta)
			}
			label, ok := v["label"].(string)
			if !ok {
				return nil, fmt.Errorf(errorFormat, kmeta)
			}
			typ, ok := v["type"].(string)
			if !ok {
				return nil, fmt.Errorf(errorFormat, kmeta)
			}
			data, ok := v["data"].(string)
			if !ok {
				return nil, fmt.Errorf(errorFormat, kmeta)
			}
			offset, ok := v["offset"].(int)
			if !ok {
				return nil, fmt.Errorf(errorFormat, kmeta)
			}
			// content
			raw, ok := v["content"].([]interface{})
			if !ok {
				return nil, fmt.Errorf(errorFormat, kmeta)
			}
			content := make([]string, len(raw))
			for i, s := range raw {
				content[i] = s.(string)
			}

			gi.Volumes[k][i] = Volume{
				Name:    name,
				Label:   label,
				Type:    typ,
				Data:    data,
				Offset:  offset,
				Content: content,
			}
		}
	}

	return gi, nil
}
