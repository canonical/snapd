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
	"strings"
	"time"

	"launchpad.net/snappy/helpers"

	"gopkg.in/yaml.v2"
)

// SnapPart represents a generic snap type
type SnapPart struct {
	name        string
	version     string
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

// NewInstalledSnapPart returns a new SnapPart from the given yamlPath
func NewInstalledSnapPart(yamlPath string) *SnapPart {
	part := SnapPart{}

	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		return nil
	}

	r, err := os.Open(yamlPath)
	if err != nil {
		log.Printf("Can not open '%s'", yamlPath)
		return nil
	}

	yamlData, err := ioutil.ReadAll(r)
	if err != nil {
		log.Printf("Can not read '%v'", r)
		return nil
	}

	var m packageYaml
	err = yaml.Unmarshal(yamlData, &m)
	if err != nil {
		log.Printf("Can not parse '%s'", yamlData)
		return nil
	}
	part.basedir = filepath.Dir(filepath.Dir(yamlPath))
	// data from the yaml
	part.name = m.Name
	part.version = m.Version
	part.isInstalled = true
	// check if the part is active
	allVersionsDir := filepath.Dir(part.basedir)
	p, _ := filepath.EvalSymlinks(filepath.Join(allVersionsDir, "current"))
	if p == part.basedir {
		part.isActive = true
	}
	part.stype = m.Type

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
	if s.stype != "" {
		return s.stype
	}
	// if not declared its a app
	return "app"
}

// Name returns the name
func (s *SnapPart) Name() string {
	return s.name
}

// Version returns the version
func (s *SnapPart) Version() string {
	return s.version
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

// Install installs the snap
func (s *SnapPart) Install(pb ProgressMeter) (err error) {
	return errors.New("Install of a local part is not possible")
}

// SetActive sets the snap active
func (s *SnapPart) SetActive() (err error) {
	return setActiveClick(s.basedir)
}

// Uninstall remove the snap from the system
func (s *SnapPart) Uninstall() (err error) {
	// OEM snaps should not be removed as they are a key
	// building block for OEMs. Prunning non active ones
	// is acceptible.
	if s.stype == SnapTypeOem && s.IsActive() {
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
func (s *SnapLocalRepository) Details(terms string) (versions []Part, err error) {
	return versions, err
}

// Updates returns the available updates
func (s *SnapLocalRepository) Updates() (parts []Part, err error) {
	return parts, err
}

// Installed returns the installed snaps from this repository
func (s *SnapLocalRepository) Installed() (parts []Part, err error) {
	globExpr := filepath.Join(s.path, "*", "*", "meta", "package.yaml")
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return parts, err
	}
	for _, yamlfile := range matches {

		// skip "current" and similar symlinks
		realpath, err := filepath.EvalSymlinks(yamlfile)
		if err != nil {
			return parts, err
		}
		if realpath != yamlfile {
			continue
		}

		snap := NewInstalledSnapPart(yamlfile)
		if snap != nil {
			parts = append(parts, snap)
		}
	}

	return parts, err
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
func (s *RemoteSnapPart) Install(pbar ProgressMeter) (err error) {
	w, err := ioutil.TempFile("", s.pkg.Name)
	if err != nil {
		return err
	}
	defer func() {
		w.Close()
		os.Remove(w.Name())
	}()

	resp, err := http.Get(s.pkg.AnonDownloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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

	err = installClick(w.Name(), 0)
	if err != nil {
		return err
	}

	return err
}

// SetActive sets the snap active
func (s *RemoteSnapPart) SetActive() (err error) {
	return errors.New("A remote part must be installed first")
}

// Uninstall remove the snap from the system
func (s *RemoteSnapPart) Uninstall() (err error) {
	return errors.New("Uninstall of a remote part is not possible")
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

// NewUbuntuStoreSnapRepository creates a new SnapUbuntuStoreRepository
func NewUbuntuStoreSnapRepository() *SnapUbuntuStoreRepository {
	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	return &SnapUbuntuStoreRepository{
		searchURI:  "https://search.apps.ubuntu.com/api/v1/search?q=%s",
		detailsURI: "https://search.apps.ubuntu.com/api/v1/package/%s",
		bulkURI:    "https://search.apps.ubuntu.com/api/v1/click-metadata"}
}

// small helper that sets the correct http headers for the ubuntu store
func setUbuntuStoreHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/hal+json")
	frameworks, _ := InstalledSnapNamesByType(SnapTypeFramework)
	frameworks = append(frameworks, "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", helpers.Architecture())
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
		return parts, err
	}

	// set headers
	setUbuntuStoreHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return parts, err
	}
	defer resp.Body.Close()

	// check statusCode
	switch {
	case resp.StatusCode == 404:
		return parts, ErrRemoteSnapNotFound
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

	return parts, err
}

// Search searches the repository for the given searchTerm
func (s *SnapUbuntuStoreRepository) Search(searchTerm string) (parts []Part, err error) {
	url := fmt.Sprintf(s.searchURI, searchTerm)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return parts, err
	}

	// set headers
	setUbuntuStoreHeaders(req)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return parts, err
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

	return parts, err
}

// Updates returns the available updates
func (s *SnapUbuntuStoreRepository) Updates() (parts []Part, err error) {
	// the store only supports apps and framworks currently, so no
	// sense in sending it our ubuntu-core snap
	installed, err := InstalledSnapNamesByType(SnapTypeApp, SnapTypeFramework)
	if err != nil || len(installed) == 0 {
		return parts, err
	}
	jsonData, err := json.Marshal(map[string][]string{"name": installed})
	if err != nil {
		return parts, err
	}

	req, err := http.NewRequest("POST", s.bulkURI, bytes.NewBuffer([]byte(jsonData)))
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
	return parts, err
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
