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
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

type GadgetInfo struct {
	Volumes map[string]GadgetVolume `yaml:"volumes,omitempty"`

	// Default configuration for snaps (snap-id => key => value).
	Defaults map[string]map[string]interface{} `yaml:"defaults,omitempty"`
}

type GadgetVolume struct {
	Schema     string            `yaml:"schema"`
	Bootloader string            `yaml:"bootloader"`
	ID         string            `yaml:"id"`
	Structure  []VolumeStructure `yaml:"structure"`
}

// TODO Offsets and sizes are strings to support unit suffixes.
// Is that a good idea? *2^N or *10^N? We'll probably want a richer
// type when we actually handle these.

type VolumeStructure struct {
	Label       string          `yaml:"label"`
	Offset      string          `yaml:"offset"`
	OffsetWrite string          `yaml:"offset-write"`
	Size        string          `yaml:"size"`
	Type        string          `yaml:"type"`
	ID          string          `yaml:"id"`
	Filesystem  string          `yaml:"filesystem"`
	Content     []VolumeContent `yaml:"content"`
}

type VolumeContent struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`

	Image       string `yaml:"image"`
	Offset      string `yaml:"offset"`
	OffsetWrite string `yaml:"offset-write"`
	Size        string `yaml:"size"`

	Unpack bool `yaml:"unpack"`
}

// ReadGadgetInfo reads the gadget specific metadata from gadget.yaml
// in the snap. classic set to true means classic rules apply,
// i.e. content/presence of gadget.yaml is fully optional.
func ReadGadgetInfo(info *Info, classic bool) (*GadgetInfo, error) {
	const errorFormat = "cannot read gadget snap details: %s"

	if info.Type != TypeGadget {
		return nil, fmt.Errorf(errorFormat, "not a gadget snap")
	}

	var gi GadgetInfo

	gadgetYamlFn := filepath.Join(info.MountDir(), "meta", "gadget.yaml")
	gmeta, err := ioutil.ReadFile(gadgetYamlFn)
	if classic && os.IsNotExist(err) {
		// gadget.yaml is optional for classic gadgets
		return &gi, nil
	}
	if err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	if err := yaml.Unmarshal(gmeta, &gi); err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	for k, v := range gi.Defaults {
		dflt, err := normalizeYamlValue(v, "config defaults")
		if err != nil {
			return nil, err
		}
		gi.Defaults[k] = dflt.(map[string]interface{})
	}

	if classic && len(gi.Volumes) == 0 {
		// volumes can be left out on classic
		// can still specify defaults though
		return &gi, nil
	}

	// basic validation
	foundBootloader := false
	for _, v := range gi.Volumes {
		if foundBootloader {
			return nil, fmt.Errorf(errorFormat, "bootloader already declared")
		}
		switch v.Bootloader {
		case "":
			return nil, fmt.Errorf(errorFormat, "bootloader cannot be empty")
		case "grub", "u-boot":
			foundBootloader = true
		default:
			return nil, fmt.Errorf(errorFormat, "bootloader must be either grub or u-boot")
		}
	}
	if !foundBootloader {
		return nil, fmt.Errorf(errorFormat, "bootloader not declared in any volume")
	}

	return &gi, nil
}
