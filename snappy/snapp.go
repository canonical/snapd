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
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v1"
)

type SnappPart struct {
	name        string
	version     string
	description string
	hash        string
	isActive    bool
	isInstalled bool
	stype       string

	basedir string
}

type packageYaml struct {
	Name    string
	Version string
	Vendor  string
	Icon    string
	Type    string
}

type remoteSnapp struct {
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
		Packages []remoteSnapp `json:"clickindex:package"`
	} `json:"_embedded"`
}

func NewInstalledSnappPart(yaml_path string) *SnappPart {
	part := SnappPart{}

	if _, err := os.Stat(yaml_path); os.IsNotExist(err) {
		log.Printf("No '%s' found", yaml_path)
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
	part.basedir = path.Dir(path.Dir(yaml_path))
	// data from the yaml
	part.name = m.Name
	part.version = m.Version
	part.isInstalled = true
	// check if the part is active
	allVersionsDir := path.Dir(part.basedir)
	p, _ := filepath.EvalSymlinks(path.Join(allVersionsDir, "current"))
	if p == part.basedir {
		part.isActive = true
	}
	part.stype = m.Type

	return &part
}

func (s *SnappPart) Type() string {
	if s.stype != "" {
		return s.stype
	}
	// if not declared its a app
	return "app"
}

func (s *SnappPart) Name() string {
	return s.name
}

func (s *SnappPart) Version() string {
	return s.version
}

func (s *SnappPart) Description() string {
	return s.description
}

func (s *SnappPart) Hash() string {
	return s.hash
}

func (s *SnappPart) IsActive() bool {
	return s.isActive
}

func (s *SnappPart) IsInstalled() bool {
	return s.isInstalled
}

func (s *SnappPart) InstalledSize() int {
	return -1
}

func (s *SnappPart) DownloadSize() int {
	return -1
}

func (s *SnappPart) Install(pb ProgressMeter) (err error) {
	return errors.New("Install of a local part is not possible")
}

func (s *SnappPart) Uninstall() (err error) {
	// FIMXE: replace with native code
	cmd := exec.Command("click", "unregister", "--all-users", s.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	return err
}

func (s *SnappPart) Config(configuration []byte) (err error) {
	return err
}

type SnappLocalRepository struct {
	path string
}

func NewLocalSnappRepository(path string) *SnappLocalRepository {
	if s, err := os.Stat(path); err != nil || !s.IsDir() {
		log.Printf("Invalid directory %s (%s)", path, err)
		return nil
	}
	return &SnappLocalRepository{path: path}
}

func (s *SnappLocalRepository) Description() string {
	return fmt.Sprintf("Snapp local repository for %s", s.path)
}

func (s *SnappLocalRepository) Search(terms string) (versions []Part, err error) {
	return versions, err
}

func (s *SnappLocalRepository) Details(terms string) (versions []Part, err error) {
	return versions, err
}

func (s *SnappLocalRepository) GetUpdates() (parts []Part, err error) {

	return parts, err
}

func (s *SnappLocalRepository) GetInstalled() (parts []Part, err error) {

	globExpr := path.Join(s.path, "*", "*", "meta", "package.yaml")
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

		snapp := NewInstalledSnappPart(yamlfile)
		if snapp != nil {
			parts = append(parts, snapp)
		}
	}

	return parts, err
}

type RemoteSnappPart struct {
	pkg remoteSnapp
}

func (s *RemoteSnappPart) Type() string {
	// FIXME: the store does not publish this info
	return "app"
}

func (s *RemoteSnappPart) Name() string {
	return s.pkg.Name
}

func (s *RemoteSnappPart) Version() string {
	return s.pkg.Version
}

func (s *RemoteSnappPart) Description() string {
	return s.pkg.Title
}

func (s *RemoteSnappPart) Hash() string {
	return "FIXME"
}

func (s *RemoteSnappPart) IsActive() bool {
	return false
}

func (s *RemoteSnappPart) IsInstalled() bool {
	return false
}

func (s *RemoteSnappPart) InstalledSize() int {
	return -1
}

func (s *RemoteSnappPart) DownloadSize() int {
	return -1
}

func (s *RemoteSnappPart) Install(pbar ProgressMeter) (err error) {
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
		pbar.Start(resp.ContentLength)
		mw := io.MultiWriter(w, pbar)
		_, err = io.Copy(mw, resp.Body)
		pbar.Finished()
	} else {
		_, err = io.Copy(w, resp.Body)
	}

	if err != nil {
		return err
	}

	cmd := exec.Command("click", "install", "--all-users", w.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return err
}

func (s *RemoteSnappPart) Uninstall() (err error) {
	return errors.New("Uninstall of a remote part is not possible")
}

func (s *RemoteSnappPart) Config(configuration []byte) (err error) {
	return err
}

func NewRemoteSnappPart(data remoteSnapp) *RemoteSnappPart {
	return &RemoteSnappPart{pkg: data}
}

type SnappUbuntuStoreRepository struct {
	searchUri  string
	detailsUri string
	bulkUri    string
}

func NewUbuntuStoreSnappRepository() *SnappUbuntuStoreRepository {
	return &SnappUbuntuStoreRepository{
		searchUri:  "https://search.apps.ubuntu.com/api/v1/search?q=%s",
		detailsUri: "https://search.apps.ubuntu.com/api/v1/package/%s",
		bulkUri:    "https://myapps.developer.ubuntu.com/dev/api/click-metadata/"}
}

func (s *SnappUbuntuStoreRepository) Description() string {
	return fmt.Sprintf("Snapp remote repository for %s", s.searchUri)
}

func (s *SnappUbuntuStoreRepository) Details(snappName string) (parts []Part, err error) {
	url := fmt.Sprintf(s.detailsUri, snappName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return parts, err
	}

	// set headers
	req.Header.Set("Accept", "application/hal+json")
	frameworks, _ := GetInstalledSnappNamesByType("framework")
	frameworks = append(frameworks, "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", getArchitecture())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return parts, err
	}
	defer resp.Body.Close()

	var detailsData remoteSnapp

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&detailsData); err != nil {
		return nil, err
	}

	snapp := NewRemoteSnappPart(detailsData)
	parts = append(parts, snapp)

	return parts, err
}

func (s *SnappUbuntuStoreRepository) Search(searchTerm string) (parts []Part, err error) {
	url := fmt.Sprintf(s.searchUri, searchTerm)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return parts, err
	}

	// set headers
	req.Header.Set("Accept", "application/hal+json")
	frameworks, _ := GetInstalledSnappNamesByType("framework")
	frameworks = append(frameworks, "ubuntu-core-15.04-dev1")
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
	req.Header.Set("X-Ubuntu-Architecture", getArchitecture())

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
		snapp := NewRemoteSnappPart(pkg)
		parts = append(parts, snapp)
	}

	return parts, err
}

func (s *SnappUbuntuStoreRepository) GetUpdates() (parts []Part, err error) {
	// the store only supports apps and framworks currently, so no
	// sense in sending it our ubuntu-core snapp
	installed, err := GetInstalledSnappNamesByType("app,framework")
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

	var updateData []remoteSnapp
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&updateData); err != nil {
		return nil, err
	}

	for _, pkg := range updateData {
		snapp := NewRemoteSnappPart(pkg)
		parts = append(parts, snapp)
	}

	return parts, nil
}

func (s *SnappUbuntuStoreRepository) GetInstalled() (parts []Part, err error) {
	return parts, err
}
