// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"

	"io/ioutil"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

// AppYaml represents an application (binary or service)
type AppYaml struct {
	// name is parent key
	Name string
	// part of the yaml
	Version string `yaml:"version"`
	Command string `yaml:"command"`
	Daemon  string `yaml:"daemon"`

	Description string          `yaml:"description,omitempty" json:"description,omitempty"`
	Stop        string          `yaml:"stop-command,omitempty"`
	PostStop    string          `yaml:"post-stop-command,omitempty"`
	StopTimeout timeout.Timeout `yaml:"stop-timeout,omitempty"`
	BusName     string          `yaml:"bus-name,omitempty"`

	// set to yes if we need to create a systemd socket for this service
	Socket       bool   `yaml:"socket,omitempty" json:"socket,omitempty"`
	ListenStream string `yaml:"listen-stream,omitempty" json:"listen-stream,omitempty"`
	SocketMode   string `yaml:"socket-mode,omitempty" json:"socket-mode,omitempty"`

	// systemd "restart" thing
	RestartCond systemd.RestartCondition `yaml:"restart-condition,omitempty" json:"restart-condition,omitempty"`

	PlugsRef []string `yaml:"plugs"`
	SlotsRef []string `yaml:"slots"`
}

type plugYaml struct {
	Interface string `yaml:"interface"`
	//SecurityDefinitions `yaml:",inline"`
}

// TODO split into payloads per package type composing the common
// elements for all snaps.
type snapYaml struct {
	Name             string
	Version          string
	LicenseAgreement string `yaml:"license-agreement,omitempty"`
	LicenseVersion   string `yaml:"license-version,omitempty"`
	Type             snap.Type
	Summary          string
	Description      string
	Architectures    []string `yaml:"architectures"`

	// Apps can be both binary or service
	Apps map[string]*AppYaml `yaml:"apps,omitempty"`

	// Plugs maps the used "interfaces" to the apps
	Plugs map[string]*plugYaml `yaml:"plugs,omitempty"`
}

func parseSnapYamlFile(yamlPath string) (*snapYaml, error) {

	yamlData, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	// legacy support sucks :-/
	hasConfig := osutil.FileExists(filepath.Join(filepath.Dir(yamlPath), "hooks", "config"))

	return parseSnapYamlData(yamlData, hasConfig)
}

func validateSnapYamlData(file string, yamlData []byte, m *snapYaml) error {
	// check mandatory fields
	missing := []string{}
	for _, name := range []string{"Name", "Version"} {
		s := getattr(m, name).(string)
		if s == "" {
			missing = append(missing, strings.ToLower(name))
		}
	}
	if len(missing) > 0 {
		return &ErrInvalidYaml{
			File: file,
			Yaml: yamlData,
			Err:  fmt.Errorf("missing required fields '%s'", strings.Join(missing, ", ")),
		}
	}

	// this is to prevent installation of legacy packages such as those that
	// contain the developer/developer in the package name.
	if strings.ContainsRune(m.Name, '.') {
		return ErrPackageNameNotSupported
	}

	// do all checks here
	for _, app := range m.Apps {
		if err := verifyAppYaml(app); err != nil {
			return err
		}
	}

	// check for "plugs"
	for _, plugs := range m.Plugs {
		if err := verifyPlugYaml(plugs); err != nil {
			return err
		}
	}

	return nil
}

func parseSnapYamlData(yamlData []byte, hasConfig bool) (*snapYaml, error) {
	var m snapYaml
	err := yaml.Unmarshal(yamlData, &m)
	if err != nil {
		return nil, &ErrInvalidYaml{File: "snap.yaml", Err: err, Yaml: yamlData}
	}

	if m.Architectures == nil {
		m.Architectures = []string{"all"}
	}

	for name, app := range m.Apps {
		if app.StopTimeout == 0 {
			app.StopTimeout = timeout.DefaultTimeout
		}
		app.Name = name
	}

	for name, plug := range m.Plugs {
		if plug.Interface == "" {
			plug.Interface = name
		}
	}

	if err := validateSnapYamlData("snap.yaml", yamlData, &m); err != nil {
		return nil, err
	}

	return &m, nil
}
