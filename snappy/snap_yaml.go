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
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/squashfs"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

// Port is used to declare the Port and Negotiable status of such port
// that is bound to a ServiceYaml.
type Port struct {
	Port       string `yaml:"port,omitempty"`
	Negotiable bool   `yaml:"negotiable,omitempty"`
}

// Ports is a representation of Internal and External ports mapped with a Port.
type Ports struct {
	Internal map[string]Port `yaml:"internal,omitempty" json:"internal,omitempty"`
	External map[string]Port `yaml:"external,omitempty" json:"external,omitempty"`
}

// ServiceYaml represents a service inside a SnapPart
type ServiceYaml struct {
	Name        string `yaml:"name" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	Start       string          `yaml:"start,omitempty" json:"start,omitempty"`
	Stop        string          `yaml:"stop,omitempty" json:"stop,omitempty"`
	PostStop    string          `yaml:"poststop,omitempty" json:"poststop,omitempty"`
	StopTimeout timeout.Timeout `yaml:"stop-timeout,omitempty" json:"stop-timeout,omitempty"`
	BusName     string          `yaml:"bus-name,omitempty" json:"bus-name,omitempty"`
	Forking     bool            `yaml:"forking,omitempty" json:"forking,omitempty"`

	// set to yes if we need to create a systemd socket for this service
	Socket       bool   `yaml:"socket,omitempty" json:"socket,omitempty"`
	ListenStream string `yaml:"listen-stream,omitempty" json:"listen-stream,omitempty"`
	SocketMode   string `yaml:"socket-mode,omitempty" json:"socket-mode,omitempty"`
	SocketUser   string `yaml:"socket-user,omitempty" json:"socket-user,omitempty"`
	SocketGroup  string `yaml:"socket-group,omitempty" json:"socket-group,omitempty"`

	// must be a pointer so that it can be "nil" and omitempty works
	Ports *Ports `yaml:"ports,omitempty" json:"ports,omitempty"`

	SecurityDefinitions `yaml:",inline"`
}

// Binary represents a single binary inside the binaries: package.yaml
type Binary struct {
	Name string `yaml:"name"`
	Exec string `yaml:"exec"`

	SecurityDefinitions `yaml:",inline"`
}

var commasplitter = regexp.MustCompile(`\s*,\s*`).Split

// deprecarch handles the vagaries of the now-deprecated
// "architecture" field of the package.yaml
type deprecarch []string

func (v *deprecarch) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var l []string

	if err := unmarshal(&l); err != nil {
		var s string
		if err := unmarshal(&s); err != nil {
			return err

		}
		l = append([]string(nil), s)
	}
	*v = deprecarch(l)

	return nil
}

type clickAppHook map[string]string

// TODO split into payloads per package type composing the common
// elements for all snaps.
type packageYaml struct {
	Name    string
	Version string
	Icon    string
	Type    pkg.Type

	// the spec allows a string or a list here *ick* so we need
	// to convert that into something sensible via reflect
	DeprecatedArchitecture deprecarch `yaml:"architecture"`
	Architectures          []string   `yaml:"architectures"`

	DeprecatedFramework string   `yaml:"framework,omitempty"`
	Frameworks          []string `yaml:"frameworks,omitempty"`

	ServiceYamls []ServiceYaml `yaml:"services,omitempty"`
	Binaries     []Binary      `yaml:"binaries,omitempty"`

	// gadget snap only
	Gadget Gadget       `yaml:"gadget,omitempty"`
	Config SystemConfig `yaml:"config,omitempty"`

	// this is a bit ugly, but right now integration is a one:one
	// mapping of click hooks
	Integration map[string]clickAppHook

	ExplicitLicenseAgreement bool   `yaml:"explicit-license-agreement,omitempty"`
	LicenseVersion           string `yaml:"license-version,omitempty"`

	// FIXME: move into a special kernel struct
	Kernel string `yaml:"kernel,omitempty"`
	Initrd string `yaml:"initrd,omitempty"`
	Dtbs   string `yaml:"dtbs,omitempty"`
}

func parsePackageYamlFile(yamlPath string) (*packageYaml, error) {

	yamlData, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	// legacy support sucks :-/
	hasConfig := helpers.FileExists(filepath.Join(filepath.Dir(yamlPath), "hooks", "config"))

	return parsePackageYamlData(yamlData, hasConfig)
}

func validatePackageYamlData(file string, yamlData []byte, m *packageYaml) error {
	// check mandatory fields
	missing := []string{}
	for _, name := range []string{"Name", "Version"} {
		s := helpers.Getattr(m, name).(string)
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
	// contain the origin/origin in the package name.
	if strings.ContainsRune(m.Name, '.') {
		return ErrPackageNameNotSupported
	}

	// do all checks here
	for _, binary := range m.Binaries {
		if err := verifyBinariesYaml(binary); err != nil {
			return err
		}
	}
	for _, service := range m.ServiceYamls {
		if err := verifyServiceYaml(service); err != nil {
			return err
		}
	}

	return nil
}

func parsePackageYamlData(yamlData []byte, hasConfig bool) (*packageYaml, error) {
	var m packageYaml
	err := yaml.Unmarshal(yamlData, &m)
	if err != nil {
		return nil, &ErrInvalidYaml{File: "package.yaml", Err: err, Yaml: yamlData}
	}

	if err := validatePackageYamlData("package.yaml", yamlData, &m); err != nil {
		return nil, err
	}

	if m.Architectures == nil {
		if m.DeprecatedArchitecture == nil {
			m.Architectures = []string{"all"}
		} else {
			m.Architectures = m.DeprecatedArchitecture
		}
	}

	if m.DeprecatedFramework != "" {
		logger.Noticef(`Use of deprecated "framework" key in yaml`)
		if len(m.Frameworks) != 0 {
			return nil, ErrInvalidFrameworkSpecInYaml
		}

		m.Frameworks = commasplitter(m.DeprecatedFramework, -1)
		m.DeprecatedFramework = ""
	}

	// For backward compatiblity we allow that there is no "exec:" line
	// in the binary definition and that its derived from the name.
	//
	// Generate the right exec line here
	for i := range m.Binaries {
		if m.Binaries[i].Exec == "" {
			m.Binaries[i].Exec = m.Binaries[i].Name
			m.Binaries[i].Name = filepath.Base(m.Binaries[i].Exec)
		}
	}

	for i := range m.ServiceYamls {
		if m.ServiceYamls[i].StopTimeout == 0 {
			m.ServiceYamls[i].StopTimeout = timeout.DefaultTimeout
		}
	}

	m.legacyIntegration(hasConfig)

	return &m, nil
}

func (m *packageYaml) qualifiedName(origin string) string {
	if m.Type == pkg.TypeFramework || m.Type == pkg.TypeGadget {
		return m.Name
	}
	return m.Name + "." + origin
}

func (m *packageYaml) checkForNameClashes() error {
	d := make(map[string]struct{})
	for _, bin := range m.Binaries {
		d[bin.Name] = struct{}{}
	}
	for _, svc := range m.ServiceYamls {
		if _, ok := d[svc.Name]; ok {
			return ErrNameClash(svc.Name)
		}
	}

	return nil
}

func (m *packageYaml) checkForPackageInstalled(origin string) error {
	part := ActiveSnapByName(m.Name)
	if part == nil {
		return nil
	}

	if part.Origin() != origin {
		return ErrPackageNameAlreadyInstalled
	}

	return nil
}

func (m *packageYaml) FrameworksForClick() string {
	fmks := addCoreFmk(m.Frameworks)

	return strings.Join(fmks, ",")
}

func (m *packageYaml) checkForFrameworks() error {
	installed, err := ActiveSnapIterByType(BareName, pkg.TypeFramework)
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
func (m *packageYaml) checkLicenseAgreement(ag agreer, d pkg.File, currentActiveDir string) error {
	if !m.ExplicitLicenseAgreement {
		return nil
	}

	if ag == nil {
		return ErrLicenseNotAccepted
	}

	license, err := d.MetaMember("license.txt")
	if err != nil || len(license) == 0 {
		return ErrLicenseNotProvided
	}

	oldM, err := parsePackageYamlFile(filepath.Join(currentActiveDir, "meta", "package.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// don't ask for the license if
	// * the previous version also asked for license confirmation, and
	// * the license version is the same
	if err == nil && oldM.ExplicitLicenseAgreement && oldM.LicenseVersion == m.LicenseVersion {
		return nil
	}

	msg := fmt.Sprintf("%s requires that you accept the following license before continuing", m.Name)
	if !ag.Agreed(msg, string(license)) {
		return ErrLicenseNotAccepted
	}

	return nil
}

// legacyIntegration sets up the Integration property of packageYaml from its other attributes
func (m *packageYaml) legacyIntegration(hasConfig bool) {
	if m.Integration != nil {
		// TODO: append "Overriding user-provided values." to the end of the blurb.
		logger.Noticef(`The "integration" key is deprecated, and all uses of "integration" should be rewritten; see https://developer.ubuntu.com/en/snappy/guides/package-metadata/ (the "binaries" and "services" sections are probably especially relevant)."`)
	} else {
		// TODO: do this always, not just when Integration is not set
		m.Integration = make(map[string]clickAppHook)
	}

	for _, v := range m.Binaries {
		hookName := filepath.Base(v.Name)

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = clickAppHook{}
		}
		// legacy click hook
		m.Integration[hookName]["bin-path"] = v.Exec
	}

	for _, v := range m.ServiceYamls {
		hookName := filepath.Base(v.Name)

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = clickAppHook{}
		}
	}

	if hasConfig {
		m.Integration["snappy-config"] = clickAppHook{"apparmor": "meta/snappy-config.apparmor"}
	}
}

func (m *packageYaml) addSquashfsMount(baseDir string, inhibitHooks bool, inter interacter) error {
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

func (m *packageYaml) removeSquashfsMount(baseDir string, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)
	unit := systemd.MountUnitPath(stripGlobalRootDir(baseDir), "mount")
	if helpers.FileExists(unit) {
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
