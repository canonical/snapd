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

type kernelYaml struct {
	Version string `yaml:"version,omitempty"`
}

type KernelInfo struct {
	Version string
}

func ReadKernelInfo(info *Info) (*KernelInfo, error) {
	const errorFormat = "cannot read kernel snap details: %s"

	kernelYamlFn := filepath.Join(info.MountDir(), "meta", "kernel.yaml")
	kmeta, err := ioutil.ReadFile(kernelYamlFn)
	if err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	var ky kernelYaml
	if err := yaml.Unmarshal(kmeta, &ky); err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	// basic validation
	if ky.Version == "" {
		return nil, fmt.Errorf(errorFormat, "missing version in kernel.yaml")
	}

	return &KernelInfo{Version: ky.Version}, nil
}
