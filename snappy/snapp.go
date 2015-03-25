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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"launchpad.net/snappy/helpers"

	"gopkg.in/yaml.v2"
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

// Service represents a service inside a SnapPart
type Service struct {
	Name        string `yaml:"name" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	Start       string `yaml:"start,omitempty" json:"start,omitempty"`
	Stop        string `yaml:"stop,omitempty" json:"stop,omitempty"`
	PostStop    string `yaml:"poststop,omitempty" json:"poststop,omitempty"`
	StopTimeout string `yaml:"stop-timeout,omitempty" json:"stop-timeout,omitempty"`

	// must be a pointer so that it can be "nil" and omitempty works
	Ports *Ports `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// Binary represents a single binary inside the binaries: package.yaml
type Binary struct {
	Name             string `yaml:"name"`
	Exec             string `yaml:"exec"`
	SecurityTemplate string `yaml:"security-template"`
	SecurityPolicy   string `yaml:"security-policy"`
}

// SnapPart represents a generic snap type
type SnapPart struct {
	m           *packageYaml
	description string
	hash        string
	isActive    bool
	isInstalled bool
	stype       SnapType

	basedir string
}

type packageYaml struct {
	Name    string
	Version string
	Vendor  string
	Icon    string
	Type    SnapType

	// the spec allows a string or a list here *ick* so we need
	// to convert that into something sensible via reflect
	DeprecatedArchitecture interface{} `yaml:"architecture"`
	Architectures          []string

	Framework string

	Services []Service `yaml:"services,omitempty"`
	Binaries []Binary  `yaml:"binaries,omitempty"`

	// oem snap only
	Store struct {
		ID string `yaml:"id,omitempty"`
	} `yaml:"store,omitempty"`

	// this is a bit ugly, but right now integration is a one:one
	// mapping of click hooks
	Integration map[string]clickAppHook
}

// the meta/hashes file, yaml so that we can extend it later with
// more/different hashes
type hashesYaml struct {
	Sha512 string
}

type remoteSnap struct {
	Publisher       string  `json:"publisher,omitempty"`
	Name            string  `json:"name"`
	Title           string  `json:"title"`
	IconURL         string  `json:"icon_url"`
	Price           float64 `json:"price,omitempty"`
	Content         string  `json:"content,omitempty"`
	RatingsAverage  float64 `json:"ratings_average,omitempty"`
	Version         string  `json:"version"`
	AnonDownloadURL string  `json:"anon_download_url, omitempty"`
	DownloadURL     string  `json:"download_url, omitempty"`
	DownloadSha512  string  `json:"download_sha512, omitempty"`
	LastUpdated     string  `json:"last_updated, omitempty"`
	DownloadSize    int64   `json:"binary_filesize, omitempty"`
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

	var m packageYaml
	err = yaml.Unmarshal(yamlData, &m)
	if err != nil {
		log.Printf("Can not parse '%s'", yamlData)
		return nil, err
	}

	// parse the architecture: field that is either a string or a list
	// or empty (yes, you read that correctly)
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

	return &m, nil
}

// NewInstalledSnapPart returns a new SnapPart from the given yamlPath
func NewInstalledSnapPart(yamlPath string) *SnapPart {
	part := SnapPart{}

	m, err := parsePackageYamlFile(yamlPath)
	if err != nil {
		return nil
	}

	part.basedir = filepath.Dir(filepath.Dir(yamlPath))
	// data from the yaml
	part.isInstalled = true
	part.m = m

	// check if the part is active
	allVersionsDir := filepath.Dir(part.basedir)
	p, _ := filepath.EvalSymlinks(filepath.Join(allVersionsDir, "current"))
	if p == part.basedir {
		part.isActive = true
	}

	// read hash, its ok if its not there, some older versions of
	// snappy did not write this file
	hashesData, err := ioutil.ReadFile(filepath.Join(part.basedir, "meta", "hashes"))
	if err == nil {
		var h hashesYaml
		err = yaml.Unmarshal(hashesData, &h)
		if err == nil {
			part.hash = h.Sha512
		}
	}

	return &part
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
	return s.description
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

// Install installs the snap
func (s *SnapPart) Install(pb ProgressMeter, flags InstallFlags) (err error) {
	return errors.New("Install of a local part is not possible")
}

// SetActive sets the snap active
func (s *SnapPart) SetActive() (err error) {
	return setActiveClick(s.basedir, false)
}

// Uninstall remove the snap from the system
func (s *SnapPart) Uninstall() (err error) {
	// OEM snaps should not be removed as they are a key
	// building block for OEMs. Prunning non active ones
	// is acceptible.
	if s.m.Type == SnapTypeOem && s.IsActive() {
		return ErrPackageNotRemovable
	}

	return removeClick(s.basedir)
}

// Config is used to to configure the snap
func (s *SnapPart) Config(configuration []byte) (new string, err error) {
	return snapConfig(s.basedir, string(configuration))
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *SnapPart) NeedsReboot() bool {
	return false
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

		snap := NewInstalledSnapPart(yamlfile)
		if snap != nil {
			parts = append(parts, snap)
		}
	}

	return parts, nil
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
	p, err := time.Parse("2006-01-02T15:04:05.000000Z", s.pkg.LastUpdated)
	if err != nil {
		return time.Time{}
	}

	return p
}

// Install installs the snap
func (s *RemoteSnapPart) Install(pbar ProgressMeter, flags InstallFlags) (err error) {
	w, err := ioutil.TempFile("", s.pkg.Name)
	if err != nil {
		return err
	}
	defer func() {
		w.Close()
		os.Remove(w.Name())
	}()

	// try anonymous download first and fallback to authenticated
	url := s.pkg.AnonDownloadURL
	if url == "" {
		url = s.pkg.DownloadURL
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	setUbuntuStoreHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Unexpected status code %v", resp.StatusCode)
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
		return err
	}

	err = installClick(w.Name(), flags)
	if err != nil {
		return err
	}

	return err
}

// SetActive sets the snap active
func (s *RemoteSnapPart) SetActive() (err error) {
	return ErrNotInstalled
}

// Uninstall remove the snap from the system
func (s *RemoteSnapPart) Uninstall() (err error) {
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

// NewRemoteSnapPart returns a new RemoteSnapPart from the given
// remoteSnap data
func NewRemoteSnapPart(data remoteSnap) *RemoteSnapPart {
	return &RemoteSnapPart{pkg: data}
}

// SnapUbuntuStoreRepository represents the ubuntu snap store
type SnapUbuntuStoreRepository struct {
	searchURI  string
	detailsURI string
	bulkURI    string
}

var (
	storeSearchURI  = "https://search.apps.ubuntu.com/api/v1/search?q=%s"
	storeDetailsURI = "https://search.apps.ubuntu.com/api/v1/package/%s"
	storeBulkURI    = "https://search.apps.ubuntu.com/api/v1/click-metadata"
)

// NewUbuntuStoreSnapRepository creates a new SnapUbuntuStoreRepository
func NewUbuntuStoreSnapRepository() *SnapUbuntuStoreRepository {
	if storeSearchURI == "" && storeDetailsURI == "" && storeBulkURI == "" {
		return nil
	}
	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	return &SnapUbuntuStoreRepository{
		searchURI:  storeSearchURI,
		detailsURI: storeDetailsURI,
		bulkURI:    storeBulkURI,
	}
}

// small helper that sets the correct http headers for the ubuntu store
func setUbuntuStoreHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/hal+json")

	// frameworks
	frameworks, _ := InstalledSnapNamesByType(SnapTypeFramework)
	frameworks = append(frameworks, "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", helpers.Architecture())

	// check if the oem part sets a custom store-id
	oems, _ := InstalledSnapsByType(SnapTypeOem)
	if len(oems) == 1 {
		storeID := oems[0].(*SnapPart).m.Store.ID
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
	url := fmt.Sprintf(s.detailsURI, snapName)
	req, err := http.NewRequest("GET", url, nil)
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
	url := fmt.Sprintf(s.searchURI, searchTerm)
	req, err := http.NewRequest("GET", url, nil)
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
	installed, err := InstalledSnapNamesByType(SnapTypeApp, SnapTypeFramework)
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
		snap := NewRemoteSnapPart(pkg)
		parts = append(parts, snap)
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
