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

	yaml "launchpad.net/goyaml"
)

var RemoteSnapNotFoundError = errors.New("Remote Snap not found")

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

type remoteSnap struct {
	Publisher       string  `json:"publisher,omitempty"`
	Name            string  `json:"name"`
	Title           string  `json:"title"`
	IconUrl         string  `json:"icon_url"`
	Price           float64 `json:"price,omitempty"`
	Content         string  `json:"content,omitempty"`
	RatingsAverage  float64 `json:"ratings_average,omitempty"`
	Version         string  `json:"version"`
	AnonDownloadUrl string  `json:"anon_download_url, omitempty"`
	DownloadUrl     string  `json:"download_url, omitempty"`
	DownloadSha512  string  `json:"download_sha512, omitempty"`
}

type searchResults struct {
	Payload struct {
		Packages []remoteSnap `json:"clickindex:package"`
	} `json:"_embedded"`
}

func NewInstalledSnapPart(yaml_path string) *SnapPart {
	part := SnapPart{}

	if _, err := os.Stat(yaml_path); os.IsNotExist(err) {
		return nil
	}

	r, err := os.Open(yaml_path)
	if err != nil {
		log.Printf("Can not open '%s'", yaml_path)
		return nil
	}

	yaml_data, err := ioutil.ReadAll(r)
	if err != nil {
		log.Printf("Can not read '%s'", r)
		return nil
	}

	var m packageYaml
	err = yaml.Unmarshal(yaml_data, &m)
	if err != nil {
		log.Printf("Can not parse '%s'", yaml_data)
		return nil
	}
	part.basedir = filepath.Dir(filepath.Dir(yaml_path))
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

	return &part
}

func (s *SnapPart) Type() SnapType {
	if s.stype != "" {
		return s.stype
	}
	// if not declared its a app
	return "app"
}

func (s *SnapPart) Name() string {
	return s.name
}

func (s *SnapPart) Version() string {
	return s.version
}

func (s *SnapPart) Description() string {
	return s.description
}

func (s *SnapPart) Hash() string {
	return s.hash
}

func (s *SnapPart) IsActive() bool {
	return s.isActive
}

func (s *SnapPart) IsInstalled() bool {
	return s.isInstalled
}

func (s *SnapPart) InstalledSize() int {
	return -1
}

func (s *SnapPart) DownloadSize() int {
	return -1
}

func (s *SnapPart) Install(pb ProgressMeter) (err error) {
	return errors.New("Install of a local part is not possible")
}

func (s *SnapPart) SetActive() (err error) {
	return setActiveClick(s.basedir)
}

func (s *SnapPart) Uninstall() (err error) {
	err = removeClick(s.basedir)
	return err
}

func (s *SnapPart) Config(configuration []byte) (err error) {
	return err
}

func (s *SnapPart) NeedsReboot() bool {
	return false
}

type SnapLocalRepository struct {
	path string
}

func NewLocalSnapRepository(path string) *SnapLocalRepository {
	if s, err := os.Stat(path); err != nil || !s.IsDir() {
		return nil
	}
	return &SnapLocalRepository{path: path}
}

func (s *SnapLocalRepository) Description() string {
	return fmt.Sprintf("Snap local repository for %s", s.path)
}

func (s *SnapLocalRepository) Search(terms string) (versions []Part, err error) {
	return versions, err
}

func (s *SnapLocalRepository) Details(terms string) (versions []Part, err error) {
	return versions, err
}

func (s *SnapLocalRepository) Updates() (parts []Part, err error) {
	return parts, err
}

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

type RemoteSnapPart struct {
	pkg remoteSnap
}

func (s *RemoteSnapPart) Type() SnapType {
	// FIXME: the store does not publish this info
	return SnapTypeApp
}

func (s *RemoteSnapPart) Name() string {
	return s.pkg.Name
}

func (s *RemoteSnapPart) Version() string {
	return s.pkg.Version
}

func (s *RemoteSnapPart) Description() string {
	return s.pkg.Title
}

func (s *RemoteSnapPart) Hash() string {
	return "FIXME"
}

func (s *RemoteSnapPart) IsActive() bool {
	return false
}

func (s *RemoteSnapPart) IsInstalled() bool {
	return false
}

func (s *RemoteSnapPart) InstalledSize() int {
	return -1
}

func (s *RemoteSnapPart) DownloadSize() int {
	return -1
}

func (s *RemoteSnapPart) Install(pbar ProgressMeter) (err error) {
	w, err := ioutil.TempFile("", s.pkg.Name)
	if err != nil {
		return err
	}
	defer func() {
		w.Close()
		os.Remove(w.Name())
	}()

	resp, err := http.Get(s.pkg.AnonDownloadUrl)
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

func (s *RemoteSnapPart) SetActive() (err error) {
	return errors.New("A remote part must be installed first")
}

func (s *RemoteSnapPart) Uninstall() (err error) {
	return errors.New("Uninstall of a remote part is not possible")
}

func (s *RemoteSnapPart) Config(configuration []byte) (err error) {
	return err
}

func (s *RemoteSnapPart) NeedsReboot() bool {
	return false
}

func NewRemoteSnapPart(data remoteSnap) *RemoteSnapPart {
	return &RemoteSnapPart{pkg: data}
}

type SnapUbuntuStoreRepository struct {
	searchUri  string
	detailsUri string
	bulkUri    string
}

func NewUbuntuStoreSnapRepository() *SnapUbuntuStoreRepository {
	return &SnapUbuntuStoreRepository{
		searchUri:  "https://search.apps.ubuntu.com/api/v1/search?q=%s",
		detailsUri: "https://search.apps.ubuntu.com/api/v1/package/%s",
		bulkUri:    "https://myapps.developer.ubuntu.com/dev/api/click-metadata/"}
}

func (s *SnapUbuntuStoreRepository) Description() string {
	return fmt.Sprintf("Snap remote repository for %s", s.searchUri)
}

func (s *SnapUbuntuStoreRepository) Details(snapName string) (parts []Part, err error) {
	url := fmt.Sprintf(s.detailsUri, snapName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return parts, err
	}

	// set headers
	req.Header.Set("Accept", "application/hal+json")
	frameworks, _ := InstalledSnapNamesByType(SnapTypeFramework)
	frameworks = append(frameworks, "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", Architecture())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return parts, err
	}
	defer resp.Body.Close()

	// check statusCode
	switch {
	case resp.StatusCode == 404:
		return parts, RemoteSnapNotFoundError
	case resp.StatusCode != 200:
		return parts, fmt.Errorf("SnapUbuntuStoreRepository: unexpected http statusCode %i for %s", resp.StatusCode, snapName)
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

func (s *SnapUbuntuStoreRepository) Search(searchTerm string) (parts []Part, err error) {
	url := fmt.Sprintf(s.searchUri, searchTerm)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return parts, err
	}

	// set headers
	req.Header.Set("Accept", "application/hal+json")
	frameworks, _ := InstalledSnapNamesByType(SnapTypeFramework)
	frameworks = append(frameworks, "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", Architecture())

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

	req, err := http.NewRequest("POST", s.bulkUri, bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
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

func (s *SnapUbuntuStoreRepository) Installed() (parts []Part, err error) {
	return parts, err
}
