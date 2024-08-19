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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
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
	// DynamicModules points to a folder containing {modules,firmware}
	// subfolders that are created by snap or component hooks. The only
	// valid value at the moment is $SNAP_DATA (or ${SNAP_DATA}).
	DynamicModules string            `yaml:"dynamic-modules,omitempty"`
	Assets         map[string]*Asset `yaml:"assets,omitempty"`
}

// DynamicModulesDir returns the directory where the kernel is expected to
// store dynamically generated modules/firmware. To find out this needs the
// kernel snap name and revision.
func (ki *Info) DynamicModulesDir(kSnapName string, ksnapRev snap.Revision) string {
	switch ki.DynamicModules {
	case "$SNAP_DATA", "${SNAP_DATA}":
		return snap.DataDir(kSnapName, ksnapRev)
	case "":
	default:
		logger.Noticef("internal error: bad dynamic-modules: %s", ki.DynamicModules)
	}
	return ""
}

// ValidAssetName is a regular expression matching valid asset name.
var ValidAssetName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-]*$")

// InfoFromKernelYaml reads the provided kernel metadata.
func InfoFromKernelYaml(kernelYaml []byte) (*Info, error) {
	var ki Info

	if err := yaml.Unmarshal(kernelYaml, &ki); err != nil {
		return nil, fmt.Errorf("cannot parse kernel metadata: %v", err)
	}

	if !strutil.ListContains([]string{"", "$SNAP_DATA", "${SNAP_DATA}"}, ki.DynamicModules) {
		return nil, fmt.Errorf("invalid value for dynamic-modules: %q (only valid value is $SNAP_DATA at the moment)", ki.DynamicModules)
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
