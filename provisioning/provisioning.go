/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package provisioning

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"

	"gopkg.in/yaml.v2"
)

const (
	// InstallYamlFile is the name of the file created by
	// ubuntu-device-flash(1), created at system installation time,
	// that contains metadata on the installation.
	//
	// XXX: Public for ubuntu-device-flash(1)
	InstallYamlFile = "install.yaml"
)

var (
	// simplify testing
	findBootloader = partition.FindBootloader
)

// ErrNoInstallYaml is emitted when InstallYamlFile does not exist.
type ErrNoInstallYaml struct {
	origErr error
}

func (e *ErrNoInstallYaml) Error() string {
	return fmt.Sprintf("failed to read provisioning data: %s", e.origErr)
}

// InstallMeta encapsulates the metadata for a system install.
type InstallMeta struct {
	Timestamp time.Time
}

// InstallTool encapsulates metadata on the tool used to create the
// system image.
type InstallTool struct {
	Name    string
	Path    string
	Version string
}

// InstallOptions summarises the options used when creating the system image.
type InstallOptions struct {
	Size          int64  `yaml:"size"`
	SizeUnit      string `yaml:"size-unit"`
	Output        string
	Channel       string
	DevicePart    string `yaml:"device-part,omitempty"`
	Gadget        string
	OS            string
	Kernel        string
	DeveloperMode bool `yaml:"developer-mode,omitempty"`
}

// InstallYaml represents 'InstallYamlFile'
//
// XXX: Public for ubuntu-device-flash
type InstallYaml struct {
	InstallMeta    `yaml:"meta"`
	InstallTool    `yaml:"tool"`
	InstallOptions `yaml:"options"`
}

func parseInstallYaml(path string) (*InstallYaml, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, &ErrNoInstallYaml{origErr: err}
	}

	return parseInstallYamlData(data)
}

func parseInstallYamlData(yamlData []byte) (*InstallYaml, error) {
	var i InstallYaml
	err := yaml.Unmarshal(yamlData, &i)
	if err != nil {
		logger.Noticef("Cannot parse install.yaml %q", yamlData)
		return nil, err
	}

	return &i, nil
}

// InDeveloperMode returns true if the image was build with --developer-mode
func InDeveloperMode() bool {
	// FIXME: this is a bit terrible, we really need a single
	//        bootloader dir like /boot or /boot/loader
	//        instead of having to query the partition code
	bootloader, err := findBootloader()
	if err != nil {
		// can only happy on systems like ubuntu classic
		// that are not full snappy systems
		return false
	}

	file := filepath.Join(bootloader.Dir(), InstallYamlFile)
	if !osutil.FileExists(file) {
		// no idea
		return false
	}

	InstallYaml, err := parseInstallYaml(file)
	if err != nil {
		// no idea
		return false
	}

	return InstallYaml.InstallOptions.DeveloperMode
}
