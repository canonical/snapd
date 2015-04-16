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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/systemd"

	"gopkg.in/yaml.v2"
)

const (
	// the postfix we append to the release that is send to the store
	// FIXME: find a better way to detect the postfix
	releasePostfix = "-core"

	// the namespace for sideloaded snaps
	sideloadedNamespace = "sideload"
)

// Port is used to declare the Port and Negotiable status of such port
// that is bound to a Service.
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
}

// SecurityDefinitions contains the common apparmor/seccomp definitions
type SecurityDefinitions struct {
	// SecurityTemplate is a template like "default"
	SecurityTemplate string `yaml:"security-template,omitempty" json:"security-template,omitempty"`
	// SecurityOverride is a override for the high level security json
	SecurityOverride *SecurityOverrideDefinition `yaml:"security-override,omitempty" json:"security-override,omitempty"`
	// SecurityPolicy is a hand-crafted low-level policy
	SecurityPolicy *SecurityOverrideDefinition `yaml:"security-policy,omitempty" json:"security-policy,omitempty"`

	// SecurityCaps is are the apparmor/seccomp capabilities for an app
	SecurityCaps []string `yaml:"caps,omitempty" json:"caps,omitempty"`
}

// Service represents a service inside a SnapPart
type Service struct {
	Name        string `yaml:"name" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	Start       string  `yaml:"start,omitempty" json:"start,omitempty"`
	Stop        string  `yaml:"stop,omitempty" json:"stop,omitempty"`
	PostStop    string  `yaml:"poststop,omitempty" json:"poststop,omitempty"`
	StopTimeout Timeout `yaml:"stop-timeout,omitempty" json:"stop-timeout,omitempty"`

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
	namespace   string
	hash        string
	isActive    bool
	isInstalled bool

	basedir string
}

var commasplitter = regexp.MustCompile(`\s*,\s*`).Split

type packageYaml struct {
	Name    string
	Version string
	Vendor  string
	Icon    string
	Type    SnapType

	// the spec allows a string or a list here *ick* so we need
	// to convert that into something sensible via reflect
	DeprecatedArchitecture interface{} `yaml:"architecture"`
	Architectures          []string    `yaml:"architectures"`

	DeprecatedFramework string   `yaml:"framework,omitempty"`
	Frameworks          []string `yaml:"frameworks,omitempty"`

	Services []Service `yaml:"services,omitempty"`
	Binaries []Binary  `yaml:"binaries,omitempty"`

	// oem snap only
	OEM struct {
		Store struct {
			ID string `yaml:"id,omitempty"`
		} `yaml:"store,omitempty"`
	} `yaml:"oem,omitempty"`
	Config SystemConfig `yaml:"config,omitempty"`

	// this is a bit ugly, but right now integration is a one:one
	// mapping of click hooks
	Integration map[string]clickAppHook

	ExplicitLicenseAgreement bool   `yaml:"explicit-license-agreement,omitempty"`
	LicenseVersion           string `yaml:"license-version,omitempty"`
}

type remoteSnap struct {
	Publisher       string             `json:"publisher,omitempty"`
	Name            string             `json:"package_name"`
	Namespace       string             `json:"origin"`
	Title           string             `json:"title"`
	IconURL         string             `json:"icon_url"`
	Prices          map[string]float64 `json:"prices,omitempty"`
	Type            string             `json:"content,omitempty"`
	RatingsAverage  float64            `json:"ratings_average,omitempty"`
	Version         string             `json:"version"`
	AnonDownloadURL string             `json:"anon_download_url, omitempty"`
	DownloadURL     string             `json:"download_url, omitempty"`
	DownloadSha512  string             `json:"download_sha512, omitempty"`
	LastUpdated     string             `json:"last_updated, omitempty"`
	DownloadSize    int64              `json:"binary_filesize, omitempty"`
	SupportURL      string             `json:"support_url"`
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

	return parsePackageYamlData(yamlData)
}

func parsePackageYamlData(yamlData []byte) (*packageYaml, error) {
	var m packageYaml
	err := yaml.Unmarshal(yamlData, &m)
	if err != nil {
		log.Printf("Can not parse '%s'", yamlData)
		return nil, err
	}

	// parse the architecture: field that is either a string or a list
	// or empty (yes, you read that correctly)
	if m.Architectures == nil {
		v := reflect.ValueOf(m.DeprecatedArchitecture)
		switch v.Kind() {
		case reflect.Invalid:
			m.Architectures = []string{"all"}
		case reflect.String:
			m.Architectures = []string{v.String()}
		case reflect.Slice:
			v2 := m.DeprecatedArchitecture.([]interface{})
			for _, arch := range v2 {
				m.Architectures = append(m.Architectures, arch.(string))
			}
		}
	}

	if m.DeprecatedFramework != "" {
		log.Printf(`Use of deprecated "framework" key in yaml`)
		if len(m.Frameworks) != 0 {
			return nil, ErrInvalidFrameworkSpecInYaml
		}

		m.Frameworks = commasplitter(m.DeprecatedFramework, -1)
		m.DeprecatedFramework = ""
	}

	// do all checks here
	for _, binary := range m.Binaries {
		if err := verifyBinariesYaml(binary); err != nil {
			return nil, err
		}
	}
	for _, service := range m.Services {
		if err := verifyServiceYaml(service); err != nil {
			return nil, err
		}
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

	for i := range m.Services {
		if m.Services[i].StopTimeout == 0 {
			m.Services[i].StopTimeout = DefaultTimeout
		}
	}

	return &m, nil
}

func (m *packageYaml) checkForNameClashes() error {
	d := make(map[string]struct{})
	for _, bin := range m.Binaries {
		d[bin.Name] = struct{}{}
	}
	for _, svc := range m.Services {
		if _, ok := d[svc.Name]; ok {
			return ErrNameClash(svc.Name)
		}
	}

	return nil
}

func (m *packageYaml) FrameworksForClick() string {
	return strings.Join(m.Frameworks, ",")
}

func (m *packageYaml) checkForFrameworks() error {
	installed, err := ActiveSnapNamesByType(SnapTypeFramework)
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

// NewInstalledSnapPart returns a new SnapPart from the given yamlPath
func NewInstalledSnapPart(yamlPath, namespace string) *SnapPart {
	m, err := parsePackageYamlFile(yamlPath)
	if err != nil {
		return nil
	}

	return NewSnapPartFromYaml(yamlPath, namespace, m)
}

// NewSnapPartFromYaml returns a new SnapPart from the given *packageYaml at yamlPath
func NewSnapPartFromYaml(yamlPath, namespace string, m *packageYaml) *SnapPart {
	part := &SnapPart{
		basedir:     filepath.Dir(filepath.Dir(yamlPath)),
		isInstalled: true,
		namespace:   namespace,
		m:           m,
	}

	// check if the part is active
	allVersionsDir := filepath.Dir(part.basedir)
	p, _ := filepath.EvalSymlinks(filepath.Join(allVersionsDir, "current"))
	if p == part.basedir {
		part.isActive = true
	}

	// read hash, its ok if its not there, some older versions of
	// snappy did not write this file
	hashesData, err := ioutil.ReadFile(filepath.Join(part.basedir, "meta", "hashes.yaml"))
	if err == nil {
		var h hashesYaml
		err = yaml.Unmarshal(hashesData, &h)
		if err == nil {
			part.hash = h.ArchiveSha512
		}
	}

	return part
}

// Type returns the type of the SnapPart (app, oem, ...)
func (s *SnapPart) Type() SnapType {
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
	return s.m.Version
}

// Description returns the description
func (s *SnapPart) Description() string {
	// TODO: implement.
	return "NOT IMPLEMENTED"
}

// Namespace returns the namespace
func (s *SnapPart) Namespace() string {
	return s.namespace
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

// Services return a list of Service the package declares
func (s *SnapPart) Services() []Service {
	return s.m.Services
}

// OemConfig return a list of packages to configure
func (s *SnapPart) OemConfig() SystemConfig {
	return s.m.Config
}

// Install installs the snap
func (s *SnapPart) Install(pb progress.Meter, flags InstallFlags) (name string, err error) {
	return "", errors.New("Install of a local part is not possible")
}

// SetActive sets the snap active
func (s *SnapPart) SetActive(pb progress.Meter) (err error) {
	return setActiveClick(s.basedir, false, pb)
}

// Uninstall remove the snap from the system
func (s *SnapPart) Uninstall(pb progress.Meter) (err error) {
	// OEM snaps should not be removed as they are a key
	// building block for OEMs. Prunning non active ones
	// is acceptible.
	if s.m.Type == SnapTypeOem && s.IsActive() {
		return ErrPackageNotRemovable
	}

	deps, err := s.DependentNames()
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		return ErrFrameworkInUse(deps)
	}

	return removeClick(s.basedir, pb)
}

// Config is used to to configure the snap
func (s *SnapPart) Config(configuration []byte) (new string, err error) {
	return snapConfig(s.basedir, s.namespace, string(configuration))
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
	if s.Type() != SnapTypeFramework {
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

var timestampUpdater = helpers.UpdateTimestamp

// UpdateAppArmorJSONTimestamp updates the timestamp on the snap's apparmor json symlink
func (s *SnapPart) UpdateAppArmorJSONTimestamp() error {
	// TODO: receive a list of policies that have changed, and only touch
	// things if we use one of those policies

	fns, err := filepath.Glob(filepath.Join(snapAppArmorDir, fmt.Sprintf("%s_*.json", s.Name())))
	if err != nil {
		return err
	}

	for _, fn := range fns {
		if err := timestampUpdater(fn); err != nil {
			return err
		}
	}

	return nil
}

type restartJob struct {
	name    string
	timeout time.Duration
}

// RefreshDependentsSecurity refreshes the security policies of dependent snaps
func (s *SnapPart) RefreshDependentsSecurity(inter interacter) error {
	deps, err := s.Dependents()
	if err != nil {
		return err
	}

	var restart []restartJob

	for _, dep := range deps {
		if err := dep.UpdateAppArmorJSONTimestamp(); err != nil {
			return err
		}
		for _, svc := range dep.Services() {
			svcName := generateServiceFileName(dep.m, svc)
			restart = append(restart, restartJob{svcName, time.Duration(svc.StopTimeout)})
		}
	}

	if err := exec.Command(aaClickHookCmd).Run(); err != nil {
		return err
	}

	sysd := systemd.New(globalRootDir, inter)
	for _, r := range restart {
		sysd.Restart(r.name, r.timeout)
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

// Search searches the local repository
func (s *SnapLocalRepository) Search(terms string) (versions []Part, err error) {
	return versions, err
}

// Details returns details for the given snap
func (s *SnapLocalRepository) Details(name string) (versions []Part, err error) {
	if !strings.ContainsRune(name, '.') {
		name += ".*"
	}

	globExpr := filepath.Join(s.path, name, "*", "meta", "package.yaml")
	parts, err := s.partsForGlobExpr(globExpr)

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

		namespace, err := namespaceFromPath(realpath)
		if err != nil {
			return nil, err
		}

		snap := NewInstalledSnapPart(yamlfile, namespace)
		if snap != nil {
			parts = append(parts, snap)
		}
	}

	return parts, nil
}

func namespaceFromPath(path string) (string, error) {
	namespace := filepath.Ext(filepath.Dir(filepath.Join(path, "..", "..")))

	if len(namespace) < 1 {
		return "", errors.New("invalid package on system")
	}

	return namespace[1:], nil
}

// RemoteSnapPart represents a snap available on the server
type RemoteSnapPart struct {
	pkg remoteSnap
}

// Type returns the type of the SnapPart (app, oem, ...)
func (s *RemoteSnapPart) Type() SnapType {
	// FIXME: the store does not publish this info
	return SnapTypeApp
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

// Namespace is the origin
func (s *RemoteSnapPart) Namespace() string {
	return s.pkg.Namespace
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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	if pbar != nil {
		pbar.Start(float64(resp.ContentLength))
		mw := io.MultiWriter(w, pbar)
		_, err = io.Copy(mw, resp.Body)
		pbar.Finished()
	} else {
		_, err = io.Copy(w, resp.Body)
	}

	if err != nil {
		return "", err
	}

	return w.Name(), w.Sync()
}

// Install installs the snap
func (s *RemoteSnapPart) Install(pbar progress.Meter, flags InstallFlags) (string, error) {
	downloadedSnap, err := s.Download(pbar)
	if err != nil {
		return "", err
	}
	defer os.Remove(downloadedSnap)

	return installClick(downloadedSnap, flags, pbar, s.Namespace())
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
	frameworks, _ := ActiveSnapNamesByType(SnapTypeFramework)
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", string(Architecture()))
	req.Header.Set("X-Ubuntu-Release", helpers.LsbRelease()+releasePostfix)

	// check if the oem part sets a custom store-id
	oems, _ := ActiveSnapsByType(SnapTypeOem)
	if len(oems) == 1 {
		storeID := oems[0].(*SnapPart).m.OEM.Store.ID
		if storeID != "" {
			req.Header.Set("X-Ubuntu-Store", storeID)
		}
	}

	// sso
	ssoToken, err := ReadStoreToken()
	if err == nil {
		req.Header.Set("Authorization", makeOauthPlaintextSignature(req, ssoToken))
	}
}

// Description describes the repository
func (s *SnapUbuntuStoreRepository) Description() string {
	return fmt.Sprintf("Snap remote repository for %s", s.searchURI)
}

// Details returns details for the given snap in this repository
func (s *SnapUbuntuStoreRepository) Details(snapName string) (parts []Part, err error) {
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

// Search searches the repository for the given searchTerm
func (s *SnapUbuntuStoreRepository) Search(searchTerm string) (parts []Part, err error) {
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

	for _, pkg := range searchData.Payload.Packages {
		snap := NewRemoteSnapPart(pkg)
		parts = append(parts, snap)
	}

	return parts, nil
}

// Updates returns the available updates
func (s *SnapUbuntuStoreRepository) Updates() (parts []Part, err error) {
	// the store only supports apps and framworks currently, so no
	// sense in sending it our ubuntu-core snap
	installed, err := ActiveSnapNamesByType(SnapTypeApp, SnapTypeFramework)
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
	snapDataDir := filepath.Join(snapDataDir, part.Name(), part.Version())
	snapEnv := map[string]string{
		"SNAP_NAME":          part.Name(),
		"SNAP_VERSION":       part.Version(),
		"SNAP_APP_PATH":      part.basedir,
		"SNAP_APP_DATA_PATH": snapDataDir,
	}

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
