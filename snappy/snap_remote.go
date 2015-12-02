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
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/oauth"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/remote"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
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

// RemoteSnapPart represents a snap available on the server
type RemoteSnapPart struct {
	pkg remote.Snap
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

// Hash returns the hash
func (s *RemoteSnapPart) Hash() string {
	return s.pkg.DownloadSha512
}

// Channel returns the channel used
func (s *RemoteSnapPart) Channel() string {
	return s.pkg.Channel
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
	if err := os.MkdirAll(dirs.SnapIconsDir, 0755); err != nil {
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

	if err := os.MkdirAll(dirs.SnapMetaDir, 0755); err != nil {
		return err
	}

	// don't worry about previous contents
	return helpers.AtomicWriteFile(RemoteManifestPath(s), content, 0644, 0)
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
func (s *RemoteSnapPart) SetActive(bool, progress.Meter) error {
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
// remote.Snap data
func NewRemoteSnapPart(data remote.Snap) *RemoteSnapPart {
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

func cpiURL() string {
	if os.Getenv("SNAPPY_USE_STAGING_CPI") != "" {
		return "https://search.apps.staging.ubuntu.com/api/v1/"
	}

	return "https://search.apps.ubuntu.com/api/v1/"
}

func init() {
	storeBaseURI, err := url.Parse(cpiURL())
	if err != nil {
		panic(err)
	}

	storeSearchURI, err = storeBaseURI.Parse("search")
	if err != nil {
		panic(err)
	}

	v := url.Values{}
	v.Set("fields", strings.Join(getStructFields(remote.Snap{}), ","))
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
	frameworks, _ := ActiveSnapIterByType(BareName, pkg.TypeFramework)
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(addCoreFmk(frameworks), ","))
	req.Header.Set("X-Ubuntu-Architecture", string(arch.UbuntuArchitecture()))
	req.Header.Set("X-Ubuntu-Release", release.String())
	req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)

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
	var detailsData remote.Snap
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
	//
	// NOTE this *will* send .sideload apps to the store.
	installed, err := ActiveSnapIterByType(fullNameWithChannel, pkg.TypeApp, pkg.TypeFramework, pkg.TypeOem, pkg.TypeOS, pkg.TypeKernel)
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

	var updateData []remote.Snap
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
