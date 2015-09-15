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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/oauth"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/policy"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/release"
	"launchpad.net/snappy/systemd"
)

const (
	// SideloadedOrigin is the (forced) origin for sideloaded snaps
	SideloadedOrigin = "sideload"
)

// SharedName is a structure that holds an Alias to the preferred package and
// the list of all the alternatives.
type SharedName struct {
	Alias Part
	Parts []Part
}

// SharedNames is a list of all packages and it's SharedName structure.
type SharedNames map[string]*SharedName

// IsAlias determines if origin is the one that is an alias for the
// shared name.
func (f *SharedName) IsAlias(origin string) bool {
	if alias := f.Alias; alias != nil {
		return alias.Origin() == origin
	}

	return false
}

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

// SecurityOverrideDefinition is used to override apparmor or seccomp
// security defaults
type SecurityOverrideDefinition struct {
	Apparmor string `yaml:"apparmor" json:"apparmor"`
	Seccomp  string `yaml:"seccomp" json:"seccomp"`
}

// SecurityPolicyDefinition is used to provide hand-crafted policy
type SecurityPolicyDefinition struct {
	Apparmor string `yaml:"apparmor" json:"apparmor"`
	Seccomp  string `yaml:"seccomp" json:"seccomp"`
}

// SecurityDefinitions contains the common apparmor/seccomp definitions
type SecurityDefinitions struct {
	// SecurityTemplate is a template like "default"
	SecurityTemplate string `yaml:"security-template,omitempty" json:"security-template,omitempty"`
	// SecurityOverride is a override for the high level security json
	SecurityOverride *SecurityOverrideDefinition `yaml:"security-override,omitempty" json:"security-override,omitempty"`
	// SecurityPolicy is a hand-crafted low-level policy
	SecurityPolicy *SecurityPolicyDefinition `yaml:"security-policy,omitempty" json:"security-policy,omitempty"`

	// SecurityCaps is are the apparmor/seccomp capabilities for an app
	SecurityCaps []string `yaml:"caps,omitempty" json:"caps,omitempty"`
}

// NeedsAppArmorUpdate checks whether the security definitions are impacted by
// changes to policies or templates.
func (sd *SecurityDefinitions) NeedsAppArmorUpdate(policies, templates map[string]bool) bool {
	if sd.SecurityPolicy != nil {
		return false
	}

	if sd.SecurityOverride != nil {
		// XXX: actually inspect the override to figure out in more detail
		return true
	}

	if templates[sd.SecurityTemplate] {
		return true
	}

	for _, cap := range sd.SecurityCaps {
		if policies[cap] {
			return true
		}
	}

	return false
}

// ServiceYaml represents a service inside a SnapPart
type ServiceYaml struct {
	Name        string `yaml:"name" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	Start       string  `yaml:"start,omitempty" json:"start,omitempty"`
	Stop        string  `yaml:"stop,omitempty" json:"stop,omitempty"`
	PostStop    string  `yaml:"poststop,omitempty" json:"poststop,omitempty"`
	StopTimeout Timeout `yaml:"stop-timeout,omitempty" json:"stop-timeout,omitempty"`
	BusName     string  `yaml:"bus-name,omitempty" json:"bus-name,omitempty"`
	Forking     bool    `yaml:"forking,omitempty" json:"forking,omitempty"`

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

// SnapPart represents a generic snap type
type SnapPart struct {
	m           *packageYaml
	remoteM     *remoteSnap
	origin      string
	hash        string
	isActive    bool
	isInstalled bool
	description string
	deb         *clickdeb.ClickDeb
	basedir     string
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

// TODO split into payloads per package type composing the common
// elements for all snaps.
type packageYaml struct {
	Name    string
	Version string
	Vendor  string
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

	// oem snap only
	OEM    OEM          `yaml:"oem,omitempty"`
	Config SystemConfig `yaml:"config,omitempty"`

	// this is a bit ugly, but right now integration is a one:one
	// mapping of click hooks
	Integration map[string]clickAppHook

	ExplicitLicenseAgreement bool   `yaml:"explicit-license-agreement,omitempty"`
	LicenseVersion           string `yaml:"license-version,omitempty"`
}

type remoteSnap struct {
	Alias           string             `json:"alias,omitempty"`
	AnonDownloadURL string             `json:"anon_download_url,omitempty"`
	DownloadSha512  string             `json:"download_sha512,omitempty"`
	Description     string             `json:"description,omitempty"`
	DownloadSize    int64              `json:"binary_filesize,omitempty"`
	DownloadURL     string             `json:"download_url,omitempty"`
	IconURL         string             `json:"icon_url"`
	LastUpdated     string             `json:"last_updated,omitempty"`
	Name            string             `json:"package_name"`
	Origin          string             `json:"origin"`
	Prices          map[string]float64 `json:"prices,omitempty"`
	Publisher       string             `json:"publisher,omitempty"`
	RatingsAverage  float64            `json:"ratings_average,omitempty"`
	SupportURL      string             `json:"support_url"`
	Title           string             `json:"title"`
	Type            pkg.Type           `json:"content,omitempty"`
	Version         string             `json:"version"`
}

type searchResults struct {
	Payload struct {
		Packages []remoteSnap `json:"clickindex:package"`
	} `json:"_embedded"`
}

func parsePackageYamlFile(yamlPath string) (*packageYaml, error) {

	yamlData, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	// legacy support sucks :-/
	hasConfig := helpers.FileExists(filepath.Join(filepath.Dir(yamlPath), "hooks", "snappy-config"))

	return parsePackageYamlData(yamlData, hasConfig)
}

func validatePackageYamlData(file string, yamlData []byte, m *packageYaml) error {
	// check mandatory fields
	missing := []string{}
	for _, name := range []string{"Name", "Version", "Vendor"} {
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
			m.ServiceYamls[i].StopTimeout = DefaultTimeout
		}
	}

	m.legacyIntegration(hasConfig)

	return &m, nil
}

func (m *packageYaml) qualifiedName(origin string) string {
	if m.Type == pkg.TypeFramework || m.Type == pkg.TypeOem {
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

	if m.Type != pkg.TypeFramework && m.Type != pkg.TypeOem {
		if part.Origin() != origin {
			return ErrPackageNameAlreadyInstalled
		}
	}

	return nil
}

func addCoreFmk(fmks []string) []string {
	fmkCore := false
	for _, a := range fmks {
		if a == "ubuntu-core-15.04-dev1" {
			fmkCore = true
			break
		}
	}

	if !fmkCore {
		fmks = append(fmks, "ubuntu-core-15.04-dev1")
	}

	return fmks
}

func (m *packageYaml) FrameworksForClick() string {
	fmks := addCoreFmk(m.Frameworks)

	return strings.Join(fmks, ",")
}

func (m *packageYaml) checkForFrameworks() error {
	installed, err := ActiveSnapNamesByType(pkg.TypeFramework)
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
func (m *packageYaml) checkLicenseAgreement(ag agreer, d *clickdeb.ClickDeb, currentActiveDir string) error {
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

func (m *packageYaml) legacyIntegrateSecDef(hookName string, s *SecurityDefinitions) {
	// see if we have a custom security policy
	if s.SecurityPolicy != nil && s.SecurityPolicy.Apparmor != "" {
		m.Integration[hookName]["apparmor-profile"] = s.SecurityPolicy.Apparmor
		return
	}

	// see if we have a security override
	if s.SecurityOverride != nil && s.SecurityOverride.Apparmor != "" {
		m.Integration[hookName]["apparmor"] = s.SecurityOverride.Apparmor
		return
	}

	// apparmor template
	m.Integration[hookName]["apparmor"] = filepath.Join("meta", hookName+".apparmor")

	return
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

		m.legacyIntegrateSecDef(hookName, &v.SecurityDefinitions)
	}

	for _, v := range m.ServiceYamls {
		hookName := filepath.Base(v.Name)

		if _, ok := m.Integration[hookName]; !ok {
			m.Integration[hookName] = clickAppHook{}
		}

		// handle the apparmor stuff
		m.legacyIntegrateSecDef(hookName, &v.SecurityDefinitions)
	}

	if hasConfig {
		m.Integration["snappy-config"] = clickAppHook{"apparmor": "meta/snappy-config.apparmor"}
	}
}

// NewInstalledSnapPart returns a new SnapPart from the given yamlPath
func NewInstalledSnapPart(yamlPath, origin string) (*SnapPart, error) {
	m, err := parsePackageYamlFile(yamlPath)
	if err != nil {
		return nil, err
	}

	part, err := NewSnapPartFromYaml(yamlPath, origin, m)
	if err != nil {
		return nil, err
	}
	part.isInstalled = true

	return part, nil
}

// NewSnapPartFromSnapFile loads a snap from the given (clickdeb) snap file.
// Caller should call Close on the clickdeb.
// TODO: expose that Close.
func NewSnapPartFromSnapFile(snapFile string, origin string, unauthOk bool) (*SnapPart, error) {
	if err := clickdeb.Verify(snapFile, unauthOk); err != nil {
		return nil, err
	}

	d, err := clickdeb.Open(snapFile)
	if err != nil {
		return nil, err
	}

	yamlData, err := d.MetaMember("package.yaml")
	if err != nil {
		return nil, err
	}

	_, err = d.MetaMember("hooks/config")
	hasConfig := err == nil

	m, err := parsePackageYamlData(yamlData, hasConfig)
	if err != nil {
		return nil, err
	}

	targetDir := snapAppsDir
	// the "oem" parts are special
	if m.Type == pkg.TypeOem {
		targetDir = snapOemDir
	}

	if origin == SideloadedOrigin {
		m.Version = helpers.NewSideloadVersion()
	}

	fullName := m.qualifiedName(origin)
	instDir := filepath.Join(targetDir, fullName, m.Version)

	return &SnapPart{
		basedir: instDir,
		origin:  origin,
		m:       m,
		deb:     d,
	}, nil
}

// NewSnapPartFromYaml returns a new SnapPart from the given *packageYaml at yamlPath
func NewSnapPartFromYaml(yamlPath, origin string, m *packageYaml) (*SnapPart, error) {
	if _, err := os.Stat(yamlPath); err != nil {
		return nil, err
	}

	part := &SnapPart{
		basedir: filepath.Dir(filepath.Dir(yamlPath)),
		origin:  origin,
		m:       m,
	}

	// override the package's idea of its version
	// because that could have been rewritten on sideload
	// and origin is empty for frameworks, even sideloaded ones.
	m.Version = filepath.Base(part.basedir)

	// check if the part is active
	allVersionsDir := filepath.Dir(part.basedir)
	p, err := filepath.EvalSymlinks(filepath.Join(allVersionsDir, "current"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if p == part.basedir {
		part.isActive = true
	}

	// get the click *title* from readme.md and use that as the *description*.
	if description, _, err := parseReadme(filepath.Join(part.basedir, "meta", "readme.md")); err == nil {
		part.description = description
	}

	// read hash, its ok if its not there, some older versions of
	// snappy did not write this file
	hashesData, err := ioutil.ReadFile(filepath.Join(part.basedir, "meta", "hashes.yaml"))
	if err != nil {
		return nil, err
	}

	var h hashesYaml
	err = yaml.Unmarshal(hashesData, &h)
	if err != nil {
		return nil, &ErrInvalidYaml{File: "hashes.yaml", Err: err, Yaml: hashesData}
	}
	part.hash = h.ArchiveSha512

	remoteManifestPath := manifestPath(part)
	if helpers.FileExists(remoteManifestPath) {
		content, err := ioutil.ReadFile(remoteManifestPath)
		if err != nil {
			return nil, err
		}

		var r remoteSnap
		if err := yaml.Unmarshal(content, &r); err != nil {
			return nil, &ErrInvalidYaml{File: remoteManifestPath, Err: err, Yaml: content}
		}
		part.remoteM = &r
	}

	return part, nil
}

// Type returns the type of the SnapPart (app, oem, ...)
func (s *SnapPart) Type() pkg.Type {
	if s.m.Type != "" {
		return s.m.Type
	}

	// if not declared its a app
	return "app"
}

// Name returns the name
func (s *SnapPart) Name() string {
	return s.m.Name
}

// Version returns the version
func (s *SnapPart) Version() string {
	if s.basedir != "" {
		return filepath.Base(s.basedir)
	}

	return s.m.Version
}

// Description returns the description
func (s *SnapPart) Description() string {
	if r := s.remoteM; r != nil {
		return r.Description
	}

	return s.description
}

// Origin returns the origin
func (s *SnapPart) Origin() string {
	if r := s.remoteM; r != nil {
		return r.Origin
	}

	if s.origin == "" {
		return SideloadedOrigin
	}

	return s.origin
}

// Vendor returns the author. Or viceversa.
func (s *SnapPart) Vendor() string {
	return s.m.Vendor
}

// Hash returns the hash
func (s *SnapPart) Hash() string {
	return s.hash
}

// Channel returns the channel used
func (s *SnapPart) Channel() string {
	// FIXME: real channel support
	return "edge"
}

// Icon returns the path to the icon
func (s *SnapPart) Icon() string {
	if helpers.FileExists(iconPath(s)) {
		return iconPath(s)
	}

	return filepath.Join(s.basedir, s.m.Icon)
}

// IsActive returns true if the snap is active
func (s *SnapPart) IsActive() bool {
	return s.isActive
}

// IsInstalled returns true if the snap is installed
func (s *SnapPart) IsInstalled() bool {
	return s.isInstalled
}

// InstalledSize returns the size of the installed snap
func (s *SnapPart) InstalledSize() int64 {
	// FIXME: cache this at install time maybe?
	totalSize := int64(0)
	f := func(_ string, info os.FileInfo, err error) error {
		totalSize += info.Size()
		return err
	}
	filepath.Walk(s.basedir, f)
	return totalSize
}

// DownloadSize returns the dowload size
func (s *SnapPart) DownloadSize() int64 {
	if r := s.remoteM; r != nil {
		return r.DownloadSize
	}

	return -1
}

// Date returns the last update date
func (s *SnapPart) Date() time.Time {
	st, err := os.Stat(s.basedir)
	if err != nil {
		return time.Time{}
	}

	return st.ModTime()
}

// ServiceYamls return a list of ServiceYamls the package declares
func (s *SnapPart) ServiceYamls() []ServiceYaml {
	return s.m.ServiceYamls
}

// Binaries return a list of BinaryDescription the package declares
func (s *SnapPart) Binaries() []Binary {
	return s.m.Binaries
}

// OemConfig return a list of packages to configure
func (s *SnapPart) OemConfig() SystemConfig {
	return s.m.Config
}

// Install installs the snap
func (s *SnapPart) Install(inter progress.Meter, flags InstallFlags) (name string, err error) {
	allowOEM := (flags & AllowOEM) != 0
	inhibitHooks := (flags & InhibitHooks) != 0

	if s.IsInstalled() {
		return "", ErrAlreadyInstalled
	}

	if err := s.CanInstall(allowOEM, inter); err != nil {
		return "", err
	}

	manifestData, err := s.deb.ControlMember("manifest")
	if err != nil {
		logger.Noticef("Snap inspect failed for %q: %v", s.Name(), err)
		return "", err
	}

	// the "oem" parts are special
	if s.Type() == pkg.TypeOem {
		if err := installOemHardwareUdevRules(s.m); err != nil {
			return "", err
		}
	}

	fullName := QualifiedName(s)
	dataDir := filepath.Join(snapDataDir, fullName, s.Version())

	var oldPart *SnapPart
	if currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(s.basedir, "..", "current")); currentActiveDir != "" {
		oldPart, err = NewInstalledSnapPart(filepath.Join(currentActiveDir, "meta", "package.yaml"), s.origin)
		if err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(s.basedir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", s.basedir, err)
		return "", err
	}

	// if anything goes wrong here we cleanup
	defer func() {
		if err != nil {
			if e := os.RemoveAll(s.basedir); e != nil && !os.IsNotExist(e) {
				logger.Noticef("Failed to remove %q: %v", s.basedir, e)
			}
		}
	}()

	// we need to call the external helper so that we can reliable drop
	// privs
	if err := s.deb.UnpackWithDropPrivs(s.basedir, globalRootDir); err != nil {
		return "", err
	}

	// legacy, the hooks (e.g. apparmor) need this. Once we converted
	// all hooks this can go away
	clickMetaDir := filepath.Join(s.basedir, ".click", "info")
	if err := os.MkdirAll(clickMetaDir, 0755); err != nil {
		return "", err
	}
	if err := writeCompatManifestJSON(clickMetaDir, manifestData, s.origin); err != nil {
		return "", err
	}

	// write the hashes now
	if err := s.deb.ExtractHashes(filepath.Join(s.basedir, "meta")); err != nil {
		return "", err
	}

	// deal with the data:
	//
	// if there was a previous version, stop it
	// from being active so that it stops running and can no longer be
	// started then copy the data
	//
	// otherwise just create a empty data dir
	if oldPart != nil {
		// we need to stop making it active
		err = oldPart.deactivate(inhibitHooks, inter)
		defer func() {
			if err != nil {
				if cerr := oldPart.activate(inhibitHooks, inter); cerr != nil {
					logger.Noticef("Setting old version back to active failed: %v", cerr)
				}
			}
		}()
		if err != nil {
			return "", err
		}

		err = copySnapData(fullName, oldPart.Version(), s.Version())
	} else {
		err = os.MkdirAll(dataDir, 0755)
	}

	defer func() {
		if err != nil {
			if cerr := removeSnapData(fullName, s.Version()); cerr != nil {
				logger.Noticef("When cleaning up data for %s %s: %v", s.Name(), s.Version(), cerr)
			}
		}
	}()

	if err != nil {
		return "", err
	}

	// and finally make active
	err = s.activate(inhibitHooks, inter)
	defer func() {
		if err != nil && oldPart != nil {
			if cerr := oldPart.activate(inhibitHooks, inter); cerr != nil {
				logger.Noticef("When setting old %s version back to active: %v", s.Name(), cerr)
			}
		}
	}()
	if err != nil {
		return "", err
	}

	// oh, one more thing: refresh the security bits
	if !inhibitHooks {
		deps, err := s.Dependents()
		if err != nil {
			return "", err
		}

		sysd := systemd.New(globalRootDir, inter)
		stopped := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName := range stopped {
					if e := sysd.Start(serviceName); e != nil {
						inter.Notify(fmt.Sprintf("unable to restart %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()

		for _, dep := range deps {
			if !dep.IsActive() {
				continue
			}
			for _, svc := range dep.ServiceYamls() {
				serviceName := filepath.Base(generateServiceFileName(dep.m, svc))
				timeout := time.Duration(svc.StopTimeout)
				if err = sysd.Stop(serviceName, timeout); err != nil {
					inter.Notify(fmt.Sprintf("unable to stop %s; aborting install: %s", serviceName, err))
					return "", err
				}
				stopped[serviceName] = timeout
			}
		}

		if err := s.RefreshDependentsSecurity(oldPart, inter); err != nil {
			return "", err
		}

		started := make(map[string]time.Duration)
		defer func() {
			if err != nil {
				for serviceName, timeout := range started {
					if e := sysd.Stop(serviceName, timeout); e != nil {
						inter.Notify(fmt.Sprintf("unable to stop %s with the old %s: %s", serviceName, s.Name(), e))
					}
				}
			}
		}()
		for serviceName, timeout := range stopped {
			if err = sysd.Start(serviceName); err != nil {
				inter.Notify(fmt.Sprintf("unable to restart %s; aborting install: %s", serviceName, err))
				return "", err
			}
			started[serviceName] = timeout
		}
	}

	return s.Name(), nil
}

// SetActive sets the snap active
func (s *SnapPart) SetActive(pb progress.Meter) (err error) {
	return s.activate(false, pb)
}

func (s *SnapPart) activate(inhibitHooks bool, inter interacter) error {
	currentActiveSymlink := filepath.Join(s.basedir, "..", "current")
	currentActiveDir, _ := filepath.EvalSymlinks(currentActiveSymlink)

	// already active, nothing to do
	if s.basedir == currentActiveDir {
		return nil
	}

	// there is already an active part
	if currentActiveDir != "" {
		// TODO: support switching origins
		oldYaml := filepath.Join(currentActiveDir, "meta", "package.yaml")
		oldPart, err := NewInstalledSnapPart(oldYaml, s.origin)
		if err != nil {
			return err
		}
		if err := oldPart.deactivate(inhibitHooks, inter); err != nil {
			return err
		}
	}

	if s.Type() == pkg.TypeFramework {
		if err := policy.Install(s.Name(), s.basedir, globalRootDir); err != nil {
			return err
		}
	}

	if err := installClickHooks(s.basedir, s.m, s.origin, inhibitHooks); err != nil {
		// cleanup the failed hooks
		removeClickHooks(s.m, s.origin, inhibitHooks)
		return err
	}

	// generate the security policy from the package.yaml
	if err := s.m.addSecurityPolicy(s.basedir); err != nil {
		return err
	}

	// add the "binaries:" from the package.yaml
	if err := s.m.addPackageBinaries(s.basedir); err != nil {
		return err
	}
	// add the "services:" from the package.yaml
	if err := s.m.addPackageServices(s.basedir, inhibitHooks, inter); err != nil {
		return err
	}

	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentActiveSymlink, err)
	}

	// symlink is relative to parent dir
	return os.Symlink(filepath.Base(s.basedir), currentActiveSymlink)
}

func (s *SnapPart) deactivate(inhibitHooks bool, inter interacter) error {
	currentSymlink := filepath.Join(s.basedir, "..", "current")

	// sanity check
	currentActiveDir, err := filepath.EvalSymlinks(currentSymlink)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSnapNotActive
		}
		return err
	}
	if s.basedir != currentActiveDir {
		return ErrSnapNotActive
	}

	// remove generated services, binaries, clickHooks, security policy
	if err := s.m.removePackageBinaries(s.basedir); err != nil {
		return err
	}

	if err := s.m.removePackageServices(s.basedir, inter); err != nil {
		return err
	}

	if err := s.m.removeSecurityPolicy(s.basedir); err != nil {
		return err
	}

	if s.Type() == pkg.TypeFramework {
		if err := policy.Remove(s.Name(), s.basedir, globalRootDir); err != nil {
			return err
		}
	}

	if err := removeClickHooks(s.m, s.origin, inhibitHooks); err != nil {
		return err
	}

	// and finally the current symlink
	if err := os.Remove(currentSymlink); err != nil {
		logger.Noticef("Failed to remove %q: %v", currentSymlink, err)
	}

	return nil
}

// Uninstall remove the snap from the system
func (s *SnapPart) Uninstall(pb progress.Meter) (err error) {
	// OEM snaps should not be removed as they are a key
	// building block for OEMs. Prunning non active ones
	// is acceptible.
	if s.m.Type == pkg.TypeOem && s.IsActive() {
		return ErrPackageNotRemovable
	}

	if IsBuiltInSoftware(s.Name()) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	deps, err := s.DependentNames()
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return ErrFrameworkInUse(deps)
	}

	if err := s.remove(pb); err != nil {
		return err
	}

	return RemoveAllHWAccess(QualifiedName(s))
}

func (s *SnapPart) remove(inter interacter) (err error) {
	// TODO[JRL]: check the logic here. I'm not sure “remove
	// everything if active, and the click hooks if not” makes
	// sense. E.g. are we removing fmk bins on fmk upgrade? Etc.
	if err := removeClickHooks(s.m, s.origin, false); err != nil {
		return err
	}

	if err := s.deactivate(false, inter); err != nil && err != ErrSnapNotActive {
		return err
	}

	err = os.RemoveAll(s.basedir)
	if err != nil {
		return err
	}

	// best effort(?)
	os.Remove(filepath.Dir(s.basedir))

	// don't fail if icon can't be removed
	if helpers.FileExists(iconPath(s)) {
		if err := os.Remove(iconPath(s)); err != nil {
			logger.Noticef("Failed to remove store icon %s: %s", iconPath(s), err)
		}
	}

	return nil
}

// Config is used to to configure the snap
func (s *SnapPart) Config(configuration []byte) (new string, err error) {
	return snapConfig(s.basedir, s.origin, string(configuration))
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *SnapPart) NeedsReboot() bool {
	return false
}

// Frameworks returns the list of frameworks needed by the snap
func (s *SnapPart) Frameworks() ([]string, error) {
	return s.m.Frameworks, nil
}

// DependentNames returns a list of the names of apps installed that
// depend on this one
//
// /!\ not part of the Part interface.
func (s *SnapPart) DependentNames() ([]string, error) {
	deps, err := s.Dependents()
	if err != nil {
		return nil, err
	}

	names := make([]string, len(deps))
	for i, dep := range deps {
		names[i] = dep.Name()
	}

	return names, nil
}

// Dependents gives the list of apps installed that depend on this one
//
// /!\ not part of the Part interface.
func (s *SnapPart) Dependents() ([]*SnapPart, error) {
	if s.Type() != pkg.TypeFramework {
		// only frameworks are depended on
		return nil, nil
	}

	var needed []*SnapPart

	installed, err := NewMetaRepository().Installed()
	if err != nil {
		return nil, err
	}

	name := s.Name()
	for _, part := range installed {
		fmks, err := part.Frameworks()
		if err != nil {
			return nil, err
		}
		for _, fmk := range fmks {
			if fmk == name {
				part, ok := part.(*SnapPart)
				if !ok {
					return nil, ErrInstalledNonSnapPart
				}
				needed = append(needed, part)
			}
		}
	}

	return needed, nil
}

// CanInstall checks whether the SnapPart passes a series of tests required for installation
func (s *SnapPart) CanInstall(allowOEM bool, inter interacter) error {
	if s.IsInstalled() {
		return ErrAlreadyInstalled
	}

	if err := s.m.checkForPackageInstalled(s.Origin()); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !helpers.IsSupportedArchitecture(s.m.Architectures) {
		return &ErrArchitectureNotSupported{s.m.Architectures}
	}

	if err := s.m.checkForNameClashes(); err != nil {
		return err
	}

	if err := s.m.checkForFrameworks(); err != nil {
		return err
	}

	if s.Type() == pkg.TypeOem {
		if !allowOEM {
			if currentOEM, err := getOem(); err == nil {
				if currentOEM.Name != s.Name() {
					return ErrOEMPackageInstall
				}
			} else {
				// there should always be an oem package now
				return ErrOEMPackageInstall
			}
		}
	}

	curr, _ := filepath.EvalSymlinks(filepath.Join(s.basedir, "..", "current"))
	if err := s.m.checkLicenseAgreement(inter, s.deb, curr); err != nil {
		return err
	}

	return nil
}

var timestampUpdater = helpers.UpdateTimestamp

func updateAppArmorJSONTimestamp(fullName, thing, version string) error {
	fn := filepath.Join(snapAppArmorDir, fmt.Sprintf("%s_%s_%s.json", fullName, thing, version))
	return timestampUpdater(fn)
}

// RequestAppArmorUpdate checks whether changes to the given policies and
// templates impacts the snap, and updates the timestamp of the relevant json
// symlinks (thus requesting aaClickHookCmd regenerate the appropriate bits).
func (s *SnapPart) RequestAppArmorUpdate(policies, templates map[string]bool) error {

	fullName := QualifiedName(s)
	for _, svc := range s.ServiceYamls() {
		if svc.NeedsAppArmorUpdate(policies, templates) {
			if err := updateAppArmorJSONTimestamp(fullName, svc.Name, s.Version()); err != nil {
				return err
			}
		}
	}
	for _, bin := range s.Binaries() {
		if bin.NeedsAppArmorUpdate(policies, templates) {
			if err := updateAppArmorJSONTimestamp(fullName, bin.Name, s.Version()); err != nil {
				return err
			}
		}
	}

	return nil
}

// RefreshDependentsSecurity refreshes the security policies of dependent snaps
func (s *SnapPart) RefreshDependentsSecurity(oldPart *SnapPart, inter interacter) (err error) {
	oldBaseDir := ""
	if oldPart != nil {
		oldBaseDir = oldPart.basedir
	}
	upPol, upTpl := policy.AppArmorDelta(oldBaseDir, s.basedir, s.Name()+"_")

	deps, err := s.Dependents()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		err := dep.RequestAppArmorUpdate(upPol, upTpl)
		if err != nil {
			return err
		}
	}

	cmd := exec.Command(aaClickHookCmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		if exitCode, err := helpers.ExitCode(err); err == nil {
			return &ErrApparmorGenerate{
				ExitCode: exitCode,
				Output:   output,
			}
		}
		return err
	}

	return nil
}

// SnapLocalRepository is the type for a local snap repository
type SnapLocalRepository struct {
	path string
}

// NewLocalSnapRepository returns a new SnapLocalRepository for the given
// path
func NewLocalSnapRepository(path string) *SnapLocalRepository {
	if s, err := os.Stat(path); err != nil || !s.IsDir() {
		return nil
	}
	return &SnapLocalRepository{path: path}
}

// Description describes the local repository
func (s *SnapLocalRepository) Description() string {
	return fmt.Sprintf("Snap local repository for %s", s.path)
}

// Details returns details for the given snap
func (s *SnapLocalRepository) Details(name string, origin string) (versions []Part, err error) {
	if origin == "" || origin == SideloadedOrigin {
		origin = "*"
	}
	appParts, err := s.partsForGlobExpr(filepath.Join(s.path, name+"."+origin, "*", "meta", "package.yaml"))
	fmkParts, err := s.partsForGlobExpr(filepath.Join(s.path, name, "*", "meta", "package.yaml"))

	parts := append(appParts, fmkParts...)

	if len(parts) == 0 {
		return nil, ErrPackageNotFound
	}

	return parts, nil
}

// Updates returns the available updates
func (s *SnapLocalRepository) Updates() (parts []Part, err error) {
	return nil, err
}

// Installed returns the installed snaps from this repository
func (s *SnapLocalRepository) Installed() (parts []Part, err error) {
	globExpr := filepath.Join(s.path, "*", "*", "meta", "package.yaml")
	return s.partsForGlobExpr(globExpr)
}

// All the parts (ie all installed + removed-but-not-purged)
//
// TODO: that thing about removed
func (s *SnapLocalRepository) All() ([]Part, error) {
	return s.Installed()
}
func (s *SnapLocalRepository) partsForGlobExpr(globExpr string) (parts []Part, err error) {
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return nil, err
	}

	for _, yamlfile := range matches {

		// skip "current" and similar symlinks
		realpath, err := filepath.EvalSymlinks(yamlfile)
		if err != nil {
			return nil, err
		}
		if realpath != yamlfile {
			continue
		}

		origin, _ := originFromYamlPath(realpath)
		snap, err := NewInstalledSnapPart(realpath, origin)
		if err != nil {
			return nil, err
		}
		parts = append(parts, snap)

	}

	return parts, nil
}

func originFromBasedir(basedir string) (s string) {
	ext := filepath.Ext(filepath.Dir(filepath.Clean(basedir)))
	if len(ext) < 2 {
		return ""
	}

	return ext[1:]
}

// originFromYamlPath *must* return "" if it's returning error.
func originFromYamlPath(path string) (string, error) {
	origin := originFromBasedir(filepath.Join(path, "..", ".."))

	if origin == "" {
		return "", ErrInvalidPart
	}

	return origin, nil
}

// RemoteSnapPart represents a snap available on the server
type RemoteSnapPart struct {
	pkg remoteSnap
}

// Type returns the type of the SnapPart (app, oem, ...)
func (s *RemoteSnapPart) Type() pkg.Type {
	return s.pkg.Type
}

// Name returns the name
func (s *RemoteSnapPart) Name() string {
	return s.pkg.Name
}

// Version returns the version
func (s *RemoteSnapPart) Version() string {
	return s.pkg.Version
}

// Description returns the description
func (s *RemoteSnapPart) Description() string {
	return s.pkg.Title
}

// Origin is the origin
func (s *RemoteSnapPart) Origin() string {
	return s.pkg.Origin
}

// Vendor is the publisher. Author. Whatever.
func (s *RemoteSnapPart) Vendor() string {
	return s.pkg.Publisher
}

// Hash returns the hash
func (s *RemoteSnapPart) Hash() string {
	return s.pkg.DownloadSha512
}

// Channel returns the channel used
func (s *RemoteSnapPart) Channel() string {
	// FIXME: real channel support, this requires server work
	return "edge"
}

// Icon returns the icon
func (s *RemoteSnapPart) Icon() string {
	return s.pkg.IconURL
}

// IsActive returns true if the snap is active
func (s *RemoteSnapPart) IsActive() bool {
	return false
}

// IsInstalled returns true if the snap is installed
func (s *RemoteSnapPart) IsInstalled() bool {
	return false
}

// InstalledSize returns the size of the installed snap
func (s *RemoteSnapPart) InstalledSize() int64 {
	return -1
}

// DownloadSize returns the dowload size
func (s *RemoteSnapPart) DownloadSize() int64 {
	return s.pkg.DownloadSize
}

// Date returns the last update time
func (s *RemoteSnapPart) Date() time.Time {
	var p time.Time
	var err error

	for _, fmt := range []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.000000Z",
	} {
		p, err = time.Parse(fmt, s.pkg.LastUpdated)
		if err == nil {
			break
		}
	}

	return p
}

// download writes an http.Request showing a progress.Meter
func download(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return &ErrDownload{Code: resp.StatusCode, URL: req.URL}
	}

	if pbar != nil {
		pbar.Start(name, float64(resp.ContentLength))
		mw := io.MultiWriter(w, pbar)
		_, err = io.Copy(mw, resp.Body)
		pbar.Finished()
	} else {
		_, err = io.Copy(w, resp.Body)
	}

	return err
}

// Download downloads the snap and returns the filename
func (s *RemoteSnapPart) Download(pbar progress.Meter) (string, error) {
	w, err := ioutil.TempFile("", s.pkg.Name)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(w.Name())
		}
	}()
	defer w.Close()

	// try anonymous download first and fallback to authenticated
	url := s.pkg.AnonDownloadURL
	if url == "" {
		url = s.pkg.DownloadURL
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	setUbuntuStoreHeaders(req)

	if err := download(s.Name(), w, req, pbar); err != nil {
		return "", err
	}

	return w.Name(), w.Sync()
}

func (s *RemoteSnapPart) downloadIcon(pbar progress.Meter) error {
	if err := os.MkdirAll(snapIconsDir, 0755); err != nil {
		return err
	}

	iconPath := iconPath(s)
	if helpers.FileExists(iconPath) {
		return nil
	}

	req, err := http.NewRequest("GET", s.Icon(), nil)
	if err != nil {
		return err
	}

	w, err := os.OpenFile(iconPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer w.Close()

	if err := download("icon for package", w, req, pbar); err != nil {
		return err
	}

	return w.Sync()
}

func (s *RemoteSnapPart) saveStoreManifest() error {
	content, err := yaml.Marshal(s.pkg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(snapMetaDir, 0755); err != nil {
		return err
	}

	// don't worry about previous contents
	return ioutil.WriteFile(manifestPath(s), content, 0644)
}

// Install installs the snap
func (s *RemoteSnapPart) Install(pbar progress.Meter, flags InstallFlags) (string, error) {
	downloadedSnap, err := s.Download(pbar)
	if err != nil {
		return "", err
	}
	defer os.Remove(downloadedSnap)

	if err := s.downloadIcon(pbar); err != nil {
		return "", err
	}

	if err := s.saveStoreManifest(); err != nil {
		return "", err
	}

	return installClick(downloadedSnap, flags, pbar, s.Origin())
}

// SetActive sets the snap active
func (s *RemoteSnapPart) SetActive(progress.Meter) error {
	return ErrNotInstalled
}

// Uninstall remove the snap from the system
func (s *RemoteSnapPart) Uninstall(progress.Meter) error {
	return ErrNotInstalled
}

// Config is used to to configure the snap
func (s *RemoteSnapPart) Config(configuration []byte) (new string, err error) {
	return "", err
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *RemoteSnapPart) NeedsReboot() bool {
	return false
}

// Frameworks returns the list of frameworks needed by the snap
func (s *RemoteSnapPart) Frameworks() ([]string, error) {
	return nil, ErrNotImplemented
}

// NewRemoteSnapPart returns a new RemoteSnapPart from the given
// remoteSnap data
func NewRemoteSnapPart(data remoteSnap) *RemoteSnapPart {
	return &RemoteSnapPart{pkg: data}
}

// SnapUbuntuStoreRepository represents the ubuntu snap store
type SnapUbuntuStoreRepository struct {
	searchURI  *url.URL
	detailsURI *url.URL
	bulkURI    string
}

var (
	storeSearchURI  *url.URL
	storeDetailsURI *url.URL
	storeBulkURI    *url.URL
)

func getStructFields(s interface{}) []string {
	st := reflect.TypeOf(s)
	num := st.NumField()
	fields := make([]string, 0, num)
	for i := 0; i < num; i++ {
		tag := st.Field(i).Tag.Get("json")
		idx := strings.IndexRune(tag, ',')
		if idx > -1 {
			tag = tag[:idx]
		}
		if tag != "" {
			fields = append(fields, tag)
		}
	}

	return fields
}

func init() {
	storeBaseURI, err := url.Parse("https://search.apps.ubuntu.com/api/v1/")
	if err != nil {
		panic(err)
	}

	storeSearchURI, err = storeBaseURI.Parse("search")
	if err != nil {
		panic(err)
	}

	v := url.Values{}
	v.Set("fields", strings.Join(getStructFields(remoteSnap{}), ","))
	storeSearchURI.RawQuery = v.Encode()

	storeDetailsURI, err = storeBaseURI.Parse("package/")

	if err != nil {
		panic(err)
	}

	storeBulkURI, err = storeBaseURI.Parse("click-metadata")
	if err != nil {
		panic(err)
	}
	storeBulkURI.RawQuery = v.Encode()
}

// NewUbuntuStoreSnapRepository creates a new SnapUbuntuStoreRepository
func NewUbuntuStoreSnapRepository() *SnapUbuntuStoreRepository {
	if storeSearchURI == nil && storeDetailsURI == nil && storeBulkURI == nil {
		return nil
	}
	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	return &SnapUbuntuStoreRepository{
		searchURI:  storeSearchURI,
		detailsURI: storeDetailsURI,
		bulkURI:    storeBulkURI.String(),
	}
}

// small helper that sets the correct http headers for the ubuntu store
func setUbuntuStoreHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/hal+json")

	// frameworks
	frameworks, _ := ActiveSnapNamesByType(pkg.TypeFramework)
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(addCoreFmk(frameworks), ","))
	req.Header.Set("X-Ubuntu-Architecture", string(Architecture()))
	req.Header.Set("X-Ubuntu-Release", release.String())

	if storeID := os.Getenv("UBUNTU_STORE_ID"); storeID != "" {
		req.Header.Set("X-Ubuntu-Store", storeID)
	} else if storeID := StoreID(); storeID != "" {
		req.Header.Set("X-Ubuntu-Store", storeID)
	}

	// sso
	ssoToken, err := ReadStoreToken()
	if err == nil {
		req.Header.Set("Authorization", oauth.MakePlaintextSignature(&ssoToken.Token))
	}
}

// Description describes the repository
func (s *SnapUbuntuStoreRepository) Description() string {
	return fmt.Sprintf("Snap remote repository for %s", s.searchURI)
}

// Details returns details for the given snap in this repository
func (s *SnapUbuntuStoreRepository) Details(name string, origin string) (parts []Part, err error) {
	snapName := name
	if origin != "" {
		snapName = name + "." + origin
	}

	url, err := s.detailsURI.Parse(snapName)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	// set headers
	setUbuntuStoreHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// check statusCode
	switch {
	case resp.StatusCode == 404:
		return nil, ErrPackageNotFound
	case resp.StatusCode != 200:
		return parts, fmt.Errorf("SnapUbuntuStoreRepository: unexpected http statusCode %v for %s", resp.StatusCode, snapName)
	}

	// and decode json
	var detailsData remoteSnap
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&detailsData); err != nil {
		return nil, err
	}

	snap := NewRemoteSnapPart(detailsData)
	parts = append(parts, snap)

	return parts, nil
}

// All (installable) parts from the store
func (s *SnapUbuntuStoreRepository) All() ([]Part, error) {
	req, err := http.NewRequest("GET", s.searchURI.String(), nil)
	if err != nil {
		return nil, err
	}

	// set headers
	setUbuntuStoreHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchData searchResults

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&searchData); err != nil {
		return nil, err
	}

	parts := make([]Part, len(searchData.Payload.Packages))
	for i, pkg := range searchData.Payload.Packages {
		parts[i] = NewRemoteSnapPart(pkg)
	}

	return parts, nil
}

// Search searches the repository for the given searchTerm
func (s *SnapUbuntuStoreRepository) Search(searchTerm string) (SharedNames, error) {
	q := s.searchURI.Query()
	q.Set("q", searchTerm)
	s.searchURI.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", s.searchURI.String(), nil)
	if err != nil {
		return nil, err
	}

	// set headers
	setUbuntuStoreHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchData searchResults

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&searchData); err != nil {
		return nil, err
	}

	sharedNames := make(SharedNames, len(searchData.Payload.Packages))
	for _, pkg := range searchData.Payload.Packages {
		snap := NewRemoteSnapPart(pkg)
		pkgName := snap.Name()

		if _, ok := sharedNames[snap.Name()]; !ok {
			sharedNames[pkgName] = new(SharedName)
		}

		sharedNames[pkgName].Parts = append(sharedNames[pkgName].Parts, snap)
		if pkg.Alias != "" {
			sharedNames[pkgName].Alias = snap
		}
	}

	return sharedNames, nil
}

// Updates returns the available updates
func (s *SnapUbuntuStoreRepository) Updates() (parts []Part, err error) {
	// the store only supports apps, oem and frameworks currently, so no
	// sense in sending it our ubuntu-core snap
	installed, err := ActiveSnapNamesByType(pkg.TypeApp, pkg.TypeFramework, pkg.TypeOem)
	if err != nil || len(installed) == 0 {
		return nil, err
	}
	jsonData, err := json.Marshal(map[string][]string{"name": installed})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", s.bulkURI, bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		return nil, err
	}
	// set headers
	setUbuntuStoreHeaders(req)
	// the updates call is a special snowflake right now
	// (see LP: #1427155)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var updateData []remoteSnap
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&updateData); err != nil {
		return nil, err
	}

	for _, pkg := range updateData {
		current := ActiveSnapByName(pkg.Name)
		if current == nil || current.Version() != pkg.Version {
			snap := NewRemoteSnapPart(pkg)
			parts = append(parts, snap)
		}
	}

	return parts, nil
}

// Installed returns the installed snaps from this repository
func (s *SnapUbuntuStoreRepository) Installed() (parts []Part, err error) {
	return nil, err
}

// makeSnapHookEnv returns an environment suitable for passing to
// os/exec.Cmd.Env
//
// The returned environment contains additional SNAP_* variables that
// are required when calling a meta/hook/ script and that will override
// any already existing SNAP_* variables in os.Environment()
func makeSnapHookEnv(part *SnapPart) (env []string) {
	desc := struct {
		AppName     string
		AppArch     string
		AppPath     string
		Version     string
		UdevAppName string
		Origin      string
	}{
		part.Name(),
		helpers.UbuntuArchitecture(),
		part.basedir,
		part.Version(),
		QualifiedName(part),
		part.Origin(),
	}
	snapEnv := helpers.MakeMapFromEnvList(helpers.GetBasicSnapEnvVars(desc))

	// merge regular env and new snapEnv
	envMap := helpers.MakeMapFromEnvList(os.Environ())
	for k, v := range snapEnv {
		envMap[k] = v
	}

	// flatten
	for k, v := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}
