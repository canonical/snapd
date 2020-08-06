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

package gadget

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// XXX: should this be in a "gadget/kernel" or "kernel" package?

type KernelAsset struct {
	Edition editionNumber `yaml:"edition,omitempty"`
	Content []string      `yaml:"content,omitempty"`
}

type KernelInfo struct {
	Assets map[string]*KernelAsset `yaml:"assets,omitempty"`
}

// KernelInfoFromKernelYaml reads the provided kernel metadata.
func KernelInfoFromKernelYaml(kernelYaml []byte) (*KernelInfo, error) {
	var ki KernelInfo

	if err := yaml.Unmarshal(kernelYaml, &ki); err != nil {
		return nil, fmt.Errorf("cannot parse kernel metadata: %v", err)
	}

	return &ki, nil
}

// ReadInfo reads the kernel specific metadata from meta/kernel.yaml
// in the snap root directory.
func ReadKernelInfo(kernelSnapRootDir string) (*KernelInfo, error) {
	p := filepath.Join(kernelSnapRootDir, "meta", "kernel.yaml")
	content, err := ioutil.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("cannot read kernel info: %v", err)
	}
	return KernelInfoFromKernelYaml(content)
}
