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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/squashfs"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

// AppYaml represents an application (binary or service)
type AppYaml struct {
	// name is partent key
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
	Interface           string `yaml:"interface"`
	SecurityDefinitions `yaml:",inline"`
}

var commasplitter = regexp.MustCompile(`\s*,\s*`).Split

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

	// FIXME: kill once we really no longer support frameworks
	Frameworks []string `yaml:"frameworks,omitempty"`

	// Apps can be both binary or service
	Apps map[string]*AppYaml `yaml:"apps,omitempty"`

	// Plugs maps the used "interfaces" to the apps
	Plugs map[string]*plugYaml `yaml:"plugs,omitempty"`

	// FIXME: clarify those

	// gadget snap only
	Gadget Gadget       `yaml:"gadget,omitempty"`
	Config SystemConfig `yaml:"config,omitempty"`

	// FIXME: move into a special kernel struct
	Kernel string `yaml:"kernel,omitempty"`
	Initrd string `yaml:"initrd,omitempty"`
	Dtbs   string `yaml:"dtbs,omitempty"`
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

func (m *snapYaml) qualifiedName(developer string) string {
	if m.Type == snap.TypeFramework || m.Type == snap.TypeGadget {
		return m.Name
	}
	return m.Name + "." + developer
}

func checkForPackageInstalled(m *snapYaml, developer string) error {
	part := ActiveSnapByName(m.Name)
	if part == nil {
		return nil
	}

	if part.Developer() != developer {
		return fmt.Errorf("package %q is already installed with developer %q your developer is %q", m.Name, part.Developer(), developer)
	}

	return nil
}

func checkForFrameworks(m *snapYaml) error {
	installed, err := ActiveSnapIterByType(BareName, snap.TypeFramework)
	if err != nil {
		return err
	}
	sort.Strings(installed)

	missing := make([]string, 0, len(m.Frameworks))

	for _, f := range m.Frameworks {
		i := sort.SearchStrings(installed, f)
		if i >= len(installed) || installed[i] != f {
			missing = append(missing, f)
		}
	}

	if len(missing) > 0 {
		return ErrMissingFrameworks(missing)
	}

	return nil
}

// checkLicenseAgreement returns nil if it's ok to proceed with installing the
// package, as deduced from the license agreement (which might involve asking
// the user), or an error that explains the reason why installation should not
// proceed.
func checkLicenseAgreement(m *snapYaml, ag agreer, d snap.File, currentActiveDir string) error {
	if m.LicenseAgreement != "explicit" {
		return nil
	}

	if ag == nil {
		return ErrLicenseNotAccepted
	}

	license, err := d.MetaMember("license.txt")
	if err != nil || len(license) == 0 {
		return ErrLicenseNotProvided
	}

	oldM, err := parseSnapYamlFile(filepath.Join(currentActiveDir, "meta", "snap.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// don't ask for the license if
	// * the previous version also asked for license confirmation, and
	// * the license version is the same
	if err == nil && (oldM.LicenseAgreement == "explicit") && oldM.LicenseVersion == m.LicenseVersion {
		return nil
	}

	msg := fmt.Sprintf("%s requires that you accept the following license before continuing", m.Name)
	if !ag.Agreed(msg, string(license)) {
		return ErrLicenseNotAccepted
	}

	return nil
}

func addSquashfsMount(m *snapYaml, baseDir string, inhibitHooks bool, inter interacter) error {
	squashfsPath := stripGlobalRootDir(squashfs.BlobPath(baseDir))
	whereDir := stripGlobalRootDir(baseDir)

	sysd := systemd.New(dirs.GlobalRootDir, inter)
	mountUnitName, err := sysd.WriteMountUnitFile(m.Name, squashfsPath, whereDir)
	if err != nil {
		return err
	}

	// we always enable the mount unit even in inhibit hooks
	if err := sysd.Enable(mountUnitName); err != nil {
		return err
	}

	if !inhibitHooks {
		return sysd.Start(mountUnitName)
	}

	return nil
}

func removeSquashfsMount(m *snapYaml, baseDir string, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)
	unit := systemd.MountUnitPath(stripGlobalRootDir(baseDir), "mount")
	if osutil.FileExists(unit) {
		// we ignore errors, nothing should stop removals
		if err := sysd.Disable(filepath.Base(unit)); err != nil {
			logger.Noticef("Failed to disable %q: %s, but continuing anyway.", unit, err)
		}
		if err := sysd.Stop(filepath.Base(unit), time.Duration(1*time.Second)); err != nil {
			logger.Noticef("Failed to stop %q: %s, but continuing anyway.", unit, err)
		}
		if err := os.Remove(unit); err != nil {
			return err
		}
	}

	return nil
}
