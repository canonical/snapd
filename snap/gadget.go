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
	Volumes map[string]volume `yaml:"volumes,omitempty"`
}

type volume struct {
	Schema     string      `yaml:"schema"`
	Bootloader string      `yaml:"bootloader"`
	ID         string      `yaml:"id"`
	Structure  []structure `yaml:"structure"`
}

type structure struct {
	Label       string    `yaml:"label"`
	Offset      int64     `yaml:"offset"`
	OffsetWrite int64     `yaml:"offset-write"`
	Size        int64     `yaml:"size"`
	Type        string    `yaml:"type"`
	ID          string    `yaml:"id"`
	Filesystem  string    `yaml:"filesystem"`
	Content     []content `yaml:"content"`
}

type content struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`

	Image       string `yaml:"image"`
	Offset      int64  `yaml:"offset"`
	OffsetWrite int64  `yaml:"offset-write"`
	Size        int64  `yaml:"size"`

	Unpack bool `yaml:"unpack"`
}

type GadgetInfo struct {
	Volumes map[string]Volume
}

type Volume struct {
	Schema     string
	Bootloader string
	ID         string
	Structure  []Structure
}

type Structure struct {
	Label       string
	Offset      int64
	OffsetWrite int64
	Size        int64
	Type        string
	ID          string
	Filesystem  string
	Content     []Content
}
type Content struct {
	Source string
	Target string

	Image       string
	Offset      int64
	OffsetWrite int64
	Size        int64

	Unpack bool
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
	foundBootloader := false
	for _, v := range gy.Volumes {
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

	gi := &GadgetInfo{
		Volumes: make(map[string]Volume),
	}
	for k, v := range gy.Volumes {
		gi.Volumes[k] = Volume{
			Schema:     v.Schema,
			Bootloader: v.Bootloader,
			ID:         v.ID,
			Structure:  make([]Structure, len(v.Structure)),
		}
		for si, sv := range v.Structure {
			gi.Volumes[k].Structure[si] = Structure{
				Label:       sv.Label,
				Offset:      sv.Offset,
				OffsetWrite: sv.OffsetWrite,
				Size:        sv.Size,
				Type:        sv.Type,
				ID:          sv.ID,
				Filesystem:  sv.Filesystem,
				Content:     make([]Content, len(sv.Content)),
			}
			for ci, cv := range sv.Content {
				gi.Volumes[k].Structure[si].Content[ci] = Content{
					Source:      cv.Source,
					Target:      cv.Target,
					Image:       cv.Image,
					Offset:      cv.Offset,
					OffsetWrite: cv.OffsetWrite,
					Size:        cv.Size,
					Unpack:      cv.Unpack,
				}
			}
		}
	}

	return gi, nil
}
