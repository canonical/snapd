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
	"log"
	"path/filepath"
	"time"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/logger"

	"gopkg.in/yaml.v2"
)

var (
	// InstallYamlFile is the name of the file created by
	// ubuntu-device-flash(1), created at system installation time,
	// that contains metadata on the installation.
	//
	// XXX: Public for ubuntu-device-flash(1)
	InstallYamlFile = "install.yaml"

	// ErrNoInstallYaml is emitted when InstallYamlFile does not exist.
	ErrNoInstallYaml = fmt.Errorf("no %s", InstallYamlFile)
)

// InstallMeta encapsulates the metadata for a system install.
type InstallMeta struct {
	Timestamp         time.Time
	InitialVersion    string `yaml:"initial-version"`
	SystemImageServer string `yaml:"system-image-server"`
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
	Oem           string
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
		return nil, ErrNoInstallYaml
	}

	return parseInstallYamlData(data)
}

func parseInstallYamlData(yamlData []byte) (*InstallYaml, error) {
	var i InstallYaml
	err := yaml.Unmarshal(yamlData, &i)
	if err != nil {
		log.Printf("Cannot parse %q", yamlData)
		return nil, err
	}

	return &i, nil
}

// IsSideLoaded determines if the system was installed using a
// custom enablement part.
func IsSideLoaded(bootloaderDir string) bool {
	file := filepath.Join(bootloaderDir, InstallYamlFile)

	if !helpers.FileExists(file) {
		// the system may have been sideloaded, but we have no
		// way of knowing :-(
		return false
	}

	InstallYaml, err := parseInstallYaml(file)
	if err != nil {
		logger.LogError(err)
		// file isn't parseable, so let's assume system is sideloaded
		return true
	}

	if InstallYaml.InstallOptions.DevicePart != "" {
		// system was created with something like:
		//
		//  "ubuntu-device-flash --device-part=unofficial-assets.tar.xz ..."
		//
		return true
	}

	return false
}
