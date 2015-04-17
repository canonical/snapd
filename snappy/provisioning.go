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

package snappy

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/logger"

	"gopkg.in/yaml.v2"
)

// Full path to file created by ubuntu-device-flash, 
// created at system installation time, that contains metadata
// on the installation.
var (
	// XXX: Public for ubuntu-device-flash
	InstallYamlFile = "/boot/install.yaml"

	ErrNoInstallYaml = errors.New(fmt.Sprintf("no %s", InstallYamlFile))
)

type InstallMeta struct {
	Timestamp time.Time
	InitialVersion string `yaml:"initial-version"`
	SystemImageServer string `yaml:"system-image-server"`
}

type InstallTool struct {
	Name string
	Path string
	Version string
}

type InstallOptions struct {
	Size int64 `yaml:"size"`
	SizeUnit string `yaml:"size-unit"`
	Output string
	Channel string
	DevicePart string `yaml:"device-part,omitempty"`
	Oem string `yaml:,omitempty"`
	DeveloperMode bool `yaml:"developer-mode,omitempty"`
}

// Represents 'InstallYamlFile'
// XXX: Public for ubuntu-device-flash
type InstallYaml struct {
	InstallMeta `yaml:"meta"`
	InstallTool `yaml:"tool"`
	InstallOptions `yaml:"options"`
}

func parseInstallYaml(path string) (*InstallYaml, error) {
	data , err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseInstallYamlData(data)
}

func parseInstallYamlData(yamlData []byte) (*InstallYaml, error) {
	var i InstallYaml
	err := yaml.Unmarshal(yamlData, &i)
	if err != nil {
		// FIXME: should use loggo!
		// FIXME: refactor all "can not parse" messages into
		// messages.go?
		log.Printf("Cannot parse %q", yamlData)
		return nil, err
	}

	return &i, nil
}

// sideLoadedSystem determines if the system was installed using a
// custom enablement part.
func sideLoadedSystem() bool {
	file := InstallYamlFile

	if !helpers.FileExists(file) {
		// the system may have been sideloaded, but we have no
		// way of knowing :-(
		return false
	}

	InstallYaml, err := parseInstallYaml(file)
	if err != nil {
		logger.LogError(err)
		return false
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
