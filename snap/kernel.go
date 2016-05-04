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

	"gopkg.in/yaml.v2"
)

// KernelYaml contains the kernel.yaml specific data
type KernelYaml struct {
	Version string `yaml:"version,omitempty"`
}

func ValidateKernelYaml(kmeta []byte) error {
	var ky KernelYaml
	if err := yaml.Unmarshal(kmeta, &ky); err != nil {
		return fmt.Errorf("info failed to parse: %s", err)
	}
	if ky.Version == "" {
		return fmt.Errorf("missing kernel version in kernel.yaml")
	}

	return nil
}
