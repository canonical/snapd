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

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/oauth"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/remote"
)

const (
	// UbuntuCoreWireProtocol is the protocol level we support when
	// communicating with the store. History:
	//  - "1": client supports squashfs snaps
	UbuntuCoreWireProtocol = "1"
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
	// FIXME: this will become a store-url assertion
	if os.Getenv("SNAPPY_FORCE_CPI_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_CPI_URL")
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

type searchResults struct {
	Payload struct {
		Packages []remote.Snap `json:"clickindex:package"`
	} `json:"_embedded"`
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
	frameworks, _ := ActiveSnapIterByType(BareName, snap.TypeFramework)
	req.Header.Set("X-Ubuntu-Frameworks", strings.Join(frameworks, ","))
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

// Snap returns the RemoteSnapPart for the given name or an error.
func (s *SnapUbuntuStoreRepository) Snap(snapName string) (*RemoteSnapPart, error) {

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
		return nil, fmt.Errorf("SnapUbuntuStoreRepository: unexpected HTTP status code %d while looking forsnap %q", resp.StatusCode, snapName)
	}

	// and decode json
	var detailsData remote.Snap
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&detailsData); err != nil {
		return nil, err
	}

	return NewRemoteSnapPart(detailsData), nil
}

// Details returns details for the given snap in this repository
func (s *SnapUbuntuStoreRepository) Details(name string, origin string) (parts []Part, err error) {
	snapName := name
	if origin != "" {
		snapName = name + "." + origin
	}
	snap, err := s.Snap(snapName)
	if err != nil {
		return nil, err
	}
	return []Part{snap}, nil
}

// All (installable) parts from the store
func (s *SnapUbuntuStoreRepository) All() ([]Part, error) {
	return s.Find("")
}

// Find (installable) parts from the store, matching the given search term.
//
// XXX: this is actually Search, but that name is taken until we clean up
// aliases
func (s *SnapUbuntuStoreRepository) Find(searchTerm string) ([]Part, error) {
	u := *s.searchURI // make a copy, so we can mutate it

	if searchTerm != "" {
		q := u.Query()
		q.Set("q", "name:"+searchTerm)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest("GET", u.String(), nil)
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
	u := *s.searchURI // make a copy, so we can mutate it
	q := u.Query()
	q.Set("q", searchTerm)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
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
	// the store only supports apps, gadget and frameworks currently, so no
	// sense in sending it our ubuntu-core snap
	//
	// NOTE this *will* send .sideload apps to the store.
	installed, err := ActiveSnapIterByType(fullNameWithChannel, snap.TypeApp, snap.TypeFramework, snap.TypeGadget, snap.TypeOS, snap.TypeKernel)
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

// Download downloads the given snap and returns its filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *SnapUbuntuStoreRepository) Download(remoteSnap *RemoteSnapPart, pbar progress.Meter) (path string, err error) {
	w, err := ioutil.TempFile("", remoteSnap.pkg.Name)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(w.Name())
			path = ""
		}
	}()

	// try anonymous download first and fallback to authenticated
	url := remoteSnap.pkg.AnonDownloadURL
	if url == "" {
		url = remoteSnap.pkg.DownloadURL
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	setUbuntuStoreHeaders(req)

	if err := download(remoteSnap.Name(), w, req, pbar); err != nil {
		return "", err
	}

	return w.Name(), w.Sync()
}

// download writes an http.Request showing a progress.Meter
var download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
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
