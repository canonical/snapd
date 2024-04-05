// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v2"
)

type Asset struct {
	// TODO: we may make this an (optional) map at some point in
	//       the future to select what things should be updated.
	//
	// Update set to true indicates that assets shall be updated.
	Update  bool     `yaml:"update,omitempty"`
	Content []string `yaml:"content,omitempty"`
}

type Info struct {
	Assets map[string]*Asset `yaml:"assets,omitempty"`
}

// ValidAssetName is a regular expression matching valid asset name.
var ValidAssetName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-]*$")

// InfoFromKernelYaml reads the provided kernel metadata.
func InfoFromKernelYaml(kernelYaml []byte) (*Info, error) {
	var ki Info

	if err := yaml.Unmarshal(kernelYaml, &ki); err != nil {
		return nil, fmt.Errorf("cannot parse kernel metadata: %v", err)
	}

	for name := range ki.Assets {
		if !ValidAssetName.MatchString(name) {
			return nil, fmt.Errorf("invalid asset name %q, please use only alphanumeric characters and dashes", name)
		}
	}

	return &ki, nil
}

// ReadInfo reads the kernel specific metadata from meta/kernel.yaml
// in the snap root directory if the file exists.
func ReadInfo(kernelSnapRootDir string) (*Info, error) {
	p := filepath.Join(kernelSnapRootDir, "meta", "kernel.yaml")
	content, err := os.ReadFile(p)
	// meta/kernel.yaml is optional so we should not error here if
	// it is missing
	if os.IsNotExist(err) {
		return &Info{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read kernel info: %v", err)
	}
	return InfoFromKernelYaml(content)
}
