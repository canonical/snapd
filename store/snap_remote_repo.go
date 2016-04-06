// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"reflect"
	"strings"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/oauth"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
)

// TODO: better/shorter names are probably in order once fewer legacy places are using this

const (
	// UbuntuCoreWireProtocol is the protocol level we support when
	// communicating with the store. History:
	//  - "1": client supports squashfs snaps
	UbuntuCoreWireProtocol = "1"
)

func infoFromRemote(d snapDetails, purchases purchasesResults, suggestedCurrency string) *snap.Info {
	info := &snap.Info{}
	info.Type = d.Type
	info.Version = d.Version
	info.OfficialName = d.Name
	info.Revision = d.Revision
	info.EditedSummary = d.Summary
	info.EditedDescription = d.Description
	info.Developer = d.Developer
	info.Channel = d.Channel
	info.Sha512 = d.DownloadSha512
	info.Size = d.DownloadSize
	info.IconURL = d.IconURL
	info.AnonDownloadURL = d.AnonDownloadURL
	info.DownloadURL = d.DownloadURL
	info.Price = getPrice(d.Prices, suggestedCurrency)
	info.RequiresPurchase = getRequiresPurchase(d.Prices, purchases)
	return info
}

// SnapUbuntuStoreConfig represents the configuration to access the snap store
type SnapUbuntuStoreConfig struct {
	SearchURI     *url.URL
	DetailsURI    *url.URL
	BulkURI       *url.URL
	AssertionsURI *url.URL
	PurchasesURI  *url.URL
}

// SnapUbuntuStoreRepository represents the ubuntu snap store
type SnapUbuntuStoreRepository struct {
	storeID       string
	searchURI     *url.URL
	detailsURI    *url.URL
	bulkURI       *url.URL
	assertionsURI *url.URL
	purchasesURI  *url.URL
	// reused http client
	client *http.Client
}

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

func useStagingCpi() bool {
	return os.Getenv("SNAPPY_USE_STAGING_CPI") != ""
}

func cpiURL() string {
	if useStagingCpi() {
		return "https://search.apps.staging.ubuntu.com/api/v1/"
	}
	// FIXME: this will become a store-url assertion
	if os.Getenv("SNAPPY_FORCE_CPI_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_CPI_URL")
	}

	return "https://search.apps.ubuntu.com/api/v1/"
}

func authURL() string {
	if useStagingCpi() {
		return "https://login.staging.ubuntu.com/api/v2"
	}
	return "https://login.ubuntu.com/api/v2"
}

func assertsURL() string {
	if os.Getenv("SNAPPY_USE_STAGING_SAS") != "" {
		return "https://assertions.staging.ubuntu.com/v1/"
	}

	if os.Getenv("SNAPPY_FORCE_SAS_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_SAS_URL")
	}

	return "https://assertions.ubuntu.com/v1/"
}

func myappsURL() string {
	if os.Getenv("SNAPPY_USE_STAGING_MYAPPS") != "" {
		return "https://myapps.developer.staging.ubuntu.com/api/2.0"
	}
	return "https://myapps.developer.ubuntu.com/api/2.0"
}

func scaURL() string {
	if useStagingCpi() {
		return "https://myapps.developer.staging.ubuntu.com/api/2.0/"
	}
	return "https://myapps.developer.ubuntu.com/api/2.0/"
}

var defaultConfig = SnapUbuntuStoreConfig{}

func init() {
	storeBaseURI, err := url.Parse(cpiURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.SearchURI, err = storeBaseURI.Parse("search")
	if err != nil {
		panic(err)
	}
	v := url.Values{}
	v.Set("fields", strings.Join(getStructFields(snapDetails{}), ","))
	defaultConfig.SearchURI.RawQuery = v.Encode()

	defaultConfig.DetailsURI, err = storeBaseURI.Parse("package/")
	if err != nil {
		panic(err)
	}

	defaultConfig.BulkURI, err = storeBaseURI.Parse("click-metadata")
	if err != nil {
		panic(err)
	}
	defaultConfig.BulkURI.RawQuery = v.Encode()

	assertsBaseURI, err := url.Parse(assertsURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.AssertionsURI, err = assertsBaseURI.Parse("assertions/")
	if err != nil {
		panic(err)
	}

	scaBaseURI, err := url.Parse(scaURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.PurchasesURI, err = scaBaseURI.Parse("click/purchases/")
	if err != nil {
		panic(err)
	}
}

type searchResults struct {
	Payload struct {
		Packages []snapDetails `json:"clickindex:package"`
	} `json:"_embedded"`
}

type purchasesResults []Purchase

// NewUbuntuStoreSnapRepository creates a new SnapUbuntuStoreRepository with the given access configuration and for given the store id.
func NewUbuntuStoreSnapRepository(cfg *SnapUbuntuStoreConfig, storeID string) *SnapUbuntuStoreRepository {
	if cfg == nil {
		cfg = &defaultConfig
	}
	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	return &SnapUbuntuStoreRepository{
		storeID:       storeID,
		searchURI:     cfg.SearchURI,
		detailsURI:    cfg.DetailsURI,
		bulkURI:       cfg.BulkURI,
		assertionsURI: cfg.AssertionsURI,
		purchasesURI:  cfg.PurchasesURI,
		client:        &http.Client{},
	}
}

// setAuthHeader sets the authorization header.
func setAuthHeader(req *http.Request, token *StoreToken) {
	if token != nil {
		req.Header.Set("Authorization", oauth.MakePlaintextSignature(&token.Token))
	}
}

// configureAuthHeader optionally sets the auth header if a token is available.
// returns true if authentication was used
func configureAuthHeader(req *http.Request) bool {
	ssoToken, err := ReadStoreToken()
	if err == nil {
		setAuthHeader(req, ssoToken)
		return true
	}
	return false
}

// small helper that sets the correct http headers for the ubuntu store
func (s *SnapUbuntuStoreRepository) applyUbuntuStoreHeaders(req *http.Request, accept string) {
	if accept == "" {
		accept = "application/hal+json"
	}
	req.Header.Set("Accept", accept)

	req.Header.Set("X-Ubuntu-Architecture", string(arch.UbuntuArchitecture()))
	req.Header.Set("X-Ubuntu-Release", release.String())
	req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)

	if s.storeID != "" {
		req.Header.Set("X-Ubuntu-Store", s.storeID)
	}
}

// small helper that sets the correct http headers for a store request including auth
// returns true if authentication was used
func (s *SnapUbuntuStoreRepository) configureStoreReq(req *http.Request, accept string) bool {
	auth := configureAuthHeader(req)
	s.applyUbuntuStoreHeaders(req, accept)
	return auth
}

func getPrice(prices map[string]float64, currency string) float64 {
	// If there are no prices, then the snap is free
	if len(prices) == 0 {
		return 0
	}

	// Look up the price by currency code
	if val, ok := prices[currency]; ok {
		return val
	}

	// Price was unavailable
	return -1
}

func getRequiresPurchase(prices map[string]float64, purchases purchasesResults) bool {
	// if the snap is free, then it doesn't need purchasing
	if len(prices) == 0 {
		return false
	}

	for _, purchase := range purchases {
		// if the purchase is not an in-app purchase
		if purchase.ItemSKU == "" {
			return false
		}
	}

	// the snap is not free, and we couldn't find a purchase
	return true
}

// Snap returns the snap.Info for the store hosted snap with the given name or an error.
func (s *SnapUbuntuStoreRepository) Snap(name, channel string) (*snap.Info, error) {
	url, err := s.detailsURI.Parse(path.Join(name, channel))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	// set headers
	s.configureStoreReq(req, "")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	purchasesData, err := s.getPurchases(name)

	// check statusCode
	switch {
	case resp.StatusCode == 404:
		return nil, ErrSnapNotFound
	case resp.StatusCode != 200:
		return nil, fmt.Errorf("SnapUbuntuStoreRepository: unexpected HTTP status code %d while looking forsnap %q/%q", resp.StatusCode, name, channel)
	}

	// and decode json
	var detailsData snapDetails
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&detailsData); err != nil {
		return nil, err
	}

	suggestedCurrency := getSuggestedCurrency(&resp.Header)

	return infoFromRemote(detailsData, purchasesData, suggestedCurrency), nil
}

func getSuggestedCurrency(h *http.Header) string {
	s := h.Get("X-Suggested-Currency")
	if s == "" {
		s = "USD"
	}
	return s
}

// FindSnaps finds  (installable) snaps from the store, matching the
func (s *SnapUbuntuStoreRepository) getPurchases(name string) (purchasesResults, error) {
	purchasesURL, err := s.purchasesURI.Parse(name + "/")
	if err != nil {
		return nil, err
	}

	q := purchasesURL.Query()
	q.Set("include_item_purchases", "true")
	purchasesURL.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", purchasesURL.String(), nil)
	if err != nil {
		return nil, err
	}

	var purchasesData purchasesResults

	// only try and run the purchases request if we used authentication
	if s.configureStoreReq(req, "") {
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusNotFound:
			break
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, ErrScaAuthFailed
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("SnapUbuntuStoreRepository: unexpected HTTP status code %d while looking for snap purcahses: %q", resp.StatusCode, name)
		default:
			dec := json.NewDecoder(resp.Body)
			if err := dec.Decode(&purchasesData); err != nil {
				return nil, err
			}
		}

	}

	return purchasesData, nil
}

func (s *SnapUbuntuStoreRepository) getAllPurchases() (map[string]purchasesResults, error) {
	req, err := http.NewRequest("GET", s.purchasesURI.String(), nil)
	if err != nil {
		return nil, err
	}

	purchasesByName := make(map[string]purchasesResults)

	// only try and run the purchases request if we used authentication
	if s.configureStoreReq(req, "") {
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrSnapNotFound
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, ErrScaAuthFailed
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("SnapUbuntuStoreRepository: unexpected HTTP status code %d while looking for purchases", resp.StatusCode)
		}

		dec := json.NewDecoder(resp.Body)

		var purchasesData purchasesResults
		if err := dec.Decode(&purchasesData); err != nil {
			return nil, err
		}

		// Index it all in a multimap
		for _, purchase := range purchasesData {
			purchasesByName[purchase.PackageName] = append(purchasesByName[purchase.PackageName], purchase)
		}
	}

	return purchasesByName, nil
}

// FindSnaps finds  (installable) parts from the store, matching the
// given search term.
func (s *SnapUbuntuStoreRepository) FindSnaps(searchTerm string, channel string) ([]*snap.Info, error) {
	if channel == "" {
		channel = release.Get().Channel
	}

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
	s.configureStoreReq(req, "")
	req.Header.Set("X-Ubuntu-Device-Channnel", channel)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	purchasesByName, err := s.getAllPurchases()

	suggestedCurrency := getSuggestedCurrency(&resp.Header)

	var searchData searchResults

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&searchData); err != nil {
		return nil, err
	}

	snaps := make([]*snap.Info, len(searchData.Payload.Packages))
	for i, pkg := range searchData.Payload.Packages {
		snaps[i] = infoFromRemote(pkg, purchasesByName[pkg.FullName], suggestedCurrency)
	}

	return snaps, nil
}

// Updates returns the available updates for a list of snap identified by fullname with channel.
func (s *SnapUbuntuStoreRepository) Updates(installed []string) (snaps []*snap.Info, err error) {
	jsonData, err := json.Marshal(map[string][]string{"name": installed})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", s.bulkURI.String(), bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		return nil, err
	}
	// set headers
	// the updates call is a special snowflake right now
	// (see LP: #1427155)
	s.configureStoreReq(req, "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	purchasesByName, err := s.getAllPurchases()

	suggestedCurrency := getSuggestedCurrency(&resp.Header)

	var updateData []snapDetails
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&updateData); err != nil {
		return nil, err
	}

	res := make([]*snap.Info, len(updateData))
	for i, rsnap := range updateData {
		res[i] = infoFromRemote(rsnap, purchasesByName[rsnap.FullName], suggestedCurrency)
	}

	return res, nil
}

// Download downloads the given snap and returns its filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *SnapUbuntuStoreRepository) Download(remoteSnap *snap.Info, pbar progress.Meter) (path string, err error) {
	w, err := ioutil.TempFile("", remoteSnap.Name())
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

	ssoToken, _ := ReadStoreToken()

	url := remoteSnap.AnonDownloadURL
	if url == "" || ssoToken != nil {
		url = remoteSnap.DownloadURL
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	setAuthHeader(req, ssoToken)
	s.applyUbuntuStoreHeaders(req, "")

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

type assertionSvcError struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Assertion retrivies the assertion for the given type and primary key.
func (s *SnapUbuntuStoreRepository) Assertion(assertType *asserts.AssertionType, primaryKey ...string) (asserts.Assertion, error) {
	url, err := s.assertionsURI.Parse(path.Join(assertType.Name, path.Join(primaryKey...)))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	configureAuthHeader(req)
	req.Header.Set("Accept", asserts.MediaType)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if resp.Header.Get("Content-Type") == "application/json" {
			var svcErr assertionSvcError
			dec := json.NewDecoder(resp.Body)
			if err := dec.Decode(&svcErr); err != nil {
				return nil, fmt.Errorf("cannot decode assertion service error with HTTP status code %d: %v", resp.StatusCode, err)
			}
			if svcErr.Status == 404 {
				return nil, ErrAssertionNotFound
			}
			return nil, fmt.Errorf("assertion service error: [%s] %q", svcErr.Title, svcErr.Detail)
		}
		return nil, fmt.Errorf("unexpected HTTP status code %d", resp.StatusCode)
	}

	// and decode assertion
	dec := asserts.NewDecoder(resp.Body)
	return dec.Decode()
}
