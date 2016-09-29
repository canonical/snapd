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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// TODO: better/shorter names are probably in order once fewer legacy places are using this

const (
	// halJsonContentType is the default accept value for store requests
	halJsonContentType = "application/hal+json"
	// jsonContentType is for store enpoints that don't support HAL
	jsonContentType = "application/json"
	// UbuntuCoreWireProtocol is the protocol level we support when
	// communicating with the store. History:
	//  - "1": client supports squashfs snaps
	UbuntuCoreWireProtocol = "1"
)

// UserAgent to send
// xxx: this should actually be set per client request, and include the client user agent
var userAgent = "unset"

func SetUserAgentFromVersion(version string) {
	extras := make([]string, 1, 3)
	extras[0] = "series " + release.Series
	if release.OnClassic {
		extras = append(extras, "classic")
	}
	if release.ReleaseInfo.ForceDevMode() {
		extras = append(extras, "devmode")
	}
	// xxx this assumes ReleaseInfo's ID and VersionID don't have weird characters
	// (see rfc 7231 for values of weird)
	// assumption checks out in practice, q.v. https://github.com/zyga/os-release-zoo
	userAgent = fmt.Sprintf("snapd/%v (%s) %s/%s (%s)", version, strings.Join(extras, "; "), release.ReleaseInfo.ID, release.ReleaseInfo.VersionID, string(arch.UbuntuArchitecture()))
}

func infoFromRemote(d snapDetails) *snap.Info {
	info := &snap.Info{}
	info.Architectures = d.Architectures
	info.Type = d.Type
	info.Version = d.Version
	info.Epoch = "0"
	info.RealName = d.Name
	info.SnapID = d.SnapID
	info.Revision = snap.R(d.Revision)
	info.EditedSummary = d.Summary
	info.EditedDescription = d.Description
	info.DeveloperID = d.DeveloperID
	info.Developer = d.Developer // XXX: obsolete, will be retired after full backfilling of DeveloperID
	info.Channel = d.Channel
	info.Sha3_384 = d.DownloadSha3_384
	info.Size = d.DownloadSize
	info.IconURL = d.IconURL
	info.AnonDownloadURL = d.AnonDownloadURL
	info.DownloadURL = d.DownloadURL
	info.Prices = d.Prices
	info.Private = d.Private
	info.Confinement = snap.ConfinementType(d.Confinement)

	deltas := make([]snap.DeltaInfo, len(d.Deltas))
	for i, d := range d.Deltas {
		deltas[i] = snap.DeltaInfo{
			FromRevision:    d.FromRevision,
			ToRevision:      d.ToRevision,
			Format:          d.Format,
			AnonDownloadURL: d.AnonDownloadURL,
			DownloadURL:     d.DownloadURL,
			Size:            d.Size,
			Sha3_384:        d.Sha3_384,
		}
	}
	info.Deltas = deltas

	screenshots := make([]snap.ScreenshotInfo, 0, len(d.ScreenshotURLs))
	for _, url := range d.ScreenshotURLs {
		screenshots = append(screenshots, snap.ScreenshotInfo{
			URL: url,
		})
	}
	info.Screenshots = screenshots

	return info
}

// Config represents the configuration to access the snap store
type Config struct {
	SearchURI         *url.URL
	DetailsURI        *url.URL
	BulkURI           *url.URL
	AssertionsURI     *url.URL
	PurchasesURI      *url.URL
	CustomersMeURI    *url.URL
	PaymentMethodsURI *url.URL

	// StoreID is the store id used if we can't get one through the AuthContext.
	StoreID string

	Architecture string
	Series       string

	DetailFields []string
	DeltaFormat  string
}

// Store represents the ubuntu snap store
type Store struct {
	searchURI         *url.URL
	detailsURI        *url.URL
	bulkURI           *url.URL
	assertionsURI     *url.URL
	purchasesURI      *url.URL
	customersMeURI    *url.URL
	paymentMethodsURI *url.URL

	architecture string
	series       string

	fallbackStoreID string

	detailFields []string
	deltaFormat  string
	// reused http client
	client *http.Client

	authContext auth.AuthContext

	mu                sync.Mutex
	suggestedCurrency string
}

func respToError(resp *http.Response, msg string) error {
	tpl := "cannot %s: got unexpected HTTP status code %d via %s to %q"
	if oops := resp.Header.Get("X-Oops-Id"); oops != "" {
		tpl += " [%s]"
		return fmt.Errorf(tpl, msg, resp.StatusCode, resp.Request.Method, resp.Request.URL, oops)
	}

	return fmt.Errorf(tpl, msg, resp.StatusCode, resp.Request.Method, resp.Request.URL)
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

func useDeltas() bool {
	return os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL") == "1"
}

func useStaging() bool {
	return os.Getenv("SNAPPY_USE_STAGING_STORE") == "1"
}

func cpiURL() string {
	if useStaging() {
		return "https://search.apps.staging.ubuntu.com/api/v1/"
	}
	// FIXME: this will become a store-url assertion
	if os.Getenv("SNAPPY_FORCE_CPI_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_CPI_URL")
	}

	return "https://search.apps.ubuntu.com/api/v1/"
}

func authLocation() string {
	if useStaging() {
		return "login.staging.ubuntu.com"
	}
	return "login.ubuntu.com"
}

func authURL() string {
	if os.Getenv("SNAPPY_FORCE_SSO_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_SSO_URL")
	}
	return "https://" + authLocation() + "/api/v2"
}

func assertsURL() string {
	if useStaging() {
		return "https://assertions.staging.ubuntu.com/v1/"
	}

	if os.Getenv("SNAPPY_FORCE_SAS_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_SAS_URL")
	}

	return "https://assertions.ubuntu.com/v1/"
}

func myappsURL() string {
	if useStaging() {
		return "https://myapps.developer.staging.ubuntu.com/"
	}
	return "https://myapps.developer.ubuntu.com/"
}

var defaultConfig = Config{}

// DefaultConfig returns a copy of the default configuration ready to be adapted.
func DefaultConfig() *Config {
	cfg := defaultConfig
	return &cfg
}

func init() {
	storeBaseURI, err := url.Parse(cpiURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.SearchURI, err = storeBaseURI.Parse("snaps/search")
	if err != nil {
		panic(err)
	}

	// slash at the end because snap name is appended to this with .Parse(snapName)
	defaultConfig.DetailsURI, err = storeBaseURI.Parse("snaps/details/")
	if err != nil {
		panic(err)
	}

	defaultConfig.BulkURI, err = storeBaseURI.Parse("snaps/metadata")
	if err != nil {
		panic(err)
	}

	assertsBaseURI, err := url.Parse(assertsURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.AssertionsURI, err = assertsBaseURI.Parse("assertions/")
	if err != nil {
		panic(err)
	}

	defaultConfig.PurchasesURI, err = url.Parse(myappsURL() + "dev/api/snap-purchases/")
	if err != nil {
		panic(err)
	}

	defaultConfig.CustomersMeURI, err = url.Parse(myappsURL() + "purchases/v1/customers/me")
	if err != nil {
		panic(err)
	}

	defaultConfig.PaymentMethodsURI, err = url.Parse(myappsURL() + "api/2.0/click/paymentmethods/")
	if err != nil {
		panic(err)
	}
}

type searchResults struct {
	Payload struct {
		Packages []snapDetails `json:"clickindex:package"`
	} `json:"_embedded"`
}

// The fields we are interested in
var detailFields = getStructFields(snapDetails{})

// The default delta format if not configured.
var defaultSupportedDeltaFormat = "xdelta"

// New creates a new Store with the given access configuration and for given the store id.
func New(cfg *Config, authContext auth.AuthContext) *Store {
	if cfg == nil {
		cfg = &defaultConfig
	}

	fields := cfg.DetailFields
	if fields == nil {
		fields = detailFields
	}

	rawQuery := ""
	if len(fields) > 0 {
		v := url.Values{}
		v.Set("fields", strings.Join(fields, ","))
		rawQuery = v.Encode()
	}

	var searchURI *url.URL
	if cfg.SearchURI != nil {
		uri := *cfg.SearchURI
		uri.RawQuery = rawQuery
		searchURI = &uri
	}

	var detailsURI *url.URL
	if cfg.DetailsURI != nil {
		uri := *cfg.DetailsURI
		uri.RawQuery = rawQuery
		detailsURI = &uri
	}

	architecture := arch.UbuntuArchitecture()
	if cfg.Architecture != "" {
		architecture = cfg.Architecture
	}

	series := release.Series
	if cfg.Series != "" {
		series = cfg.Series
	}

	deltaFormat := cfg.DeltaFormat
	if deltaFormat == "" {
		deltaFormat = defaultSupportedDeltaFormat
	}

	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	return &Store{
		searchURI:         searchURI,
		detailsURI:        detailsURI,
		bulkURI:           cfg.BulkURI,
		assertionsURI:     cfg.AssertionsURI,
		purchasesURI:      cfg.PurchasesURI,
		customersMeURI:    cfg.CustomersMeURI,
		paymentMethodsURI: cfg.PaymentMethodsURI,
		series:            series,
		architecture:      architecture,
		fallbackStoreID:   cfg.StoreID,
		detailFields:      fields,
		client:            newHTTPClient(),
		authContext:       authContext,
		deltaFormat:       deltaFormat,
	}
}

// LoginUser logs user in the store and returns the authentication macaroons.
func LoginUser(username, password, otp string) (string, string, error) {
	macaroon, err := requestStoreMacaroon()
	if err != nil {
		return "", "", err
	}
	deserializedMacaroon, err := MacaroonDeserialize(macaroon)
	if err != nil {
		return "", "", err
	}

	// get SSO 3rd party caveat, and request discharge
	loginCaveat, err := loginCaveatID(deserializedMacaroon)
	if err != nil {
		return "", "", err
	}

	discharge, err := dischargeAuthCaveat(loginCaveat, username, password, otp)
	if err != nil {
		return "", "", err
	}

	return macaroon, discharge, nil
}

// authenticateUser will add the store expected Macaroon Authorization header for user
func authenticateUser(r *http.Request, user *auth.UserState) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `Macaroon root="%s"`, user.StoreMacaroon)

	// deserialize root macaroon (we need its signature to do the discharge binding)
	root, err := MacaroonDeserialize(user.StoreMacaroon)
	if err != nil {
		logger.Debugf("cannot deserialize root macaroon: %v", err)
		return
	}

	for _, d := range user.StoreDischarges {
		// prepare discharge for request
		discharge, err := MacaroonDeserialize(d)
		if err != nil {
			logger.Debugf("cannot deserialize discharge macaroon: %v", err)
			return
		}
		discharge.Bind(root.Signature())

		serializedDischarge, err := MacaroonSerialize(discharge)
		if err != nil {
			logger.Debugf("cannot re-serialize discharge macaroon: %v", err)
			return
		}
		fmt.Fprintf(&buf, `, discharge="%s"`, serializedDischarge)
	}
	r.Header.Set("Authorization", buf.String())
}

// refreshDischarges will request refreshed discharge macaroons for the user
func refreshDischarges(user *auth.UserState) ([]string, error) {
	newDischarges := make([]string, len(user.StoreDischarges))
	for i, d := range user.StoreDischarges {
		discharge, err := MacaroonDeserialize(d)
		if err != nil {
			return nil, err
		}
		if discharge.Location() != UbuntuoneLocation {
			newDischarges[i] = d
			continue
		}

		refreshedDischarge, err := refreshDischargeMacaroon(d)
		if err != nil {
			return nil, err
		}
		newDischarges[i] = refreshedDischarge
	}
	return newDischarges, nil
}

// refreshUser will refresh user discharge macaroon and update state
func (s *Store) refreshUser(user *auth.UserState) error {
	newDischarges, err := refreshDischarges(user)
	if err != nil {
		return err
	}

	if s.authContext != nil {
		curUser, err := s.authContext.UpdateUserAuth(user, newDischarges)
		if err != nil {
			return err
		}
		// update in place
		*user = *curUser
	}

	return nil
}

// refreshDeviceSession will set or refresh the device session in the state
func (s *Store) refreshDeviceSession(device *auth.DeviceState) error {
	if s.authContext == nil {
		return fmt.Errorf("internal error: no authContext")
	}

	nonce, err := requestStoreDeviceNonce()
	if err != nil {
		return err
	}

	sessionRequest, serialAssertion, err := s.authContext.DeviceSessionRequest(nonce)
	if err != nil {
		return err
	}

	session, err := requestDeviceSession(string(serialAssertion), string(sessionRequest), device.SessionMacaroon)
	if err != nil {
		return err
	}

	curDevice, err := s.authContext.UpdateDeviceAuth(device, session)
	if err != nil {
		return err
	}
	// update in place
	*device = *curDevice
	return nil
}

// authenticateDevice will add the store expected Macaroon X-Device-Authorization header for device
func authenticateDevice(r *http.Request, device *auth.DeviceState) {
	if device.SessionMacaroon != "" {
		r.Header.Set("X-Device-Authorization", fmt.Sprintf(`Macaroon root="%s"`, device.SessionMacaroon))
	}
}

func (s *Store) setStoreID(r *http.Request) {
	storeID := s.fallbackStoreID
	if s.authContext != nil {
		cand, err := s.authContext.StoreID(storeID)
		if err != nil {
			logger.Debugf("cannot get store ID from state: %v", err)
		} else {
			storeID = cand
		}
	}
	if storeID != "" {
		r.Header.Set("X-Ubuntu-Store", storeID)
	}
}

// requestOptions specifies parameters for store requests.
type requestOptions struct {
	Method       string
	URL          *url.URL
	Accept       string
	ContentType  string
	ExtraHeaders map[string]string
	Data         []byte
}

// doRequest does an authenticated request to the store handling a potential macaroon refresh required if needed
func (s *Store) doRequest(client *http.Client, reqOptions *requestOptions, user *auth.UserState) (*http.Response, error) {
	req, err := s.newRequest(reqOptions, user)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode == 401 {
		refreshed := false
		if user != nil && strings.Contains(wwwAuth, "needs_refresh=1") {
			// refresh user
			err = s.refreshUser(user)
			if err != nil {
				return nil, err
			}
			refreshed = true
		}
		if strings.Contains(wwwAuth, "refresh_device_session=1") {
			// refresh device session
			if s.authContext == nil {
				return nil, fmt.Errorf("internal error: no authContext")
			}
			device, err := s.authContext.Device()
			if err != nil {
				return nil, err
			}

			err = s.refreshDeviceSession(device)
			if err != nil {
				return nil, err
			}
			refreshed = true
		}
		if refreshed {
			// close previous response and retry
			// TODO: make this non-recursive or add a recursion limit
			resp.Body.Close()
			return s.doRequest(client, reqOptions, user)
		}
	}

	return resp, err
}

// build a new http.Request with headers for the store
func (s *Store) newRequest(reqOptions *requestOptions, user *auth.UserState) (*http.Request, error) {
	var body io.Reader
	if reqOptions.Data != nil {
		body = bytes.NewBuffer(reqOptions.Data)
	}

	req, err := http.NewRequest(reqOptions.Method, reqOptions.URL.String(), body)
	if err != nil {
		return nil, err
	}

	if s.authContext != nil {
		device, err := s.authContext.Device()
		if err != nil {
			return nil, err
		}
		// we don't have a session yet but have a serial, try
		// to get a session
		if device.SessionMacaroon == "" && device.Serial != "" {
			err = s.refreshDeviceSession(device)
			if err == auth.ErrNoSerial {
				// missing serial assertion, log and continue without device authentication
				logger.Debugf("cannot set device session: %v", err)
			}
			if err != nil && err != auth.ErrNoSerial {
				return nil, err
			}
		}
		authenticateDevice(req, device)
	}

	if user != nil {
		authenticateUser(req, user)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", reqOptions.Accept)
	req.Header.Set("X-Ubuntu-Architecture", s.architecture)
	req.Header.Set("X-Ubuntu-Series", s.series)
	req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)

	if reqOptions.ContentType != "" {
		req.Header.Set("Content-Type", reqOptions.ContentType)
	}

	for header, value := range reqOptions.ExtraHeaders {
		req.Header.Set(header, value)
	}

	s.setStoreID(req)

	return req, nil
}

func (s *Store) extractSuggestedCurrency(resp *http.Response) {
	suggestedCurrency := resp.Header.Get("X-Suggested-Currency")

	if suggestedCurrency != "" {
		s.mu.Lock()
		s.suggestedCurrency = suggestedCurrency
		s.mu.Unlock()
	}
}

// purchase encapsulates the purchase data sent to us from the software center agent.
//
// When making a purchase request, the State "InProgress", together with a RedirectTo
// URL may be received. In-this case, the user must be directed to that webpage in
// order to complete the purchase (e.g. to enter 3D-secure credentials).
// Additionally, Partner ID may be recieved as an extended header "X-Partner-Id",
// this should be included in the follow-on requests to the redirect URL.
//
// HTTP/1.1 200 OK
// Content-Type: application/json; charset=utf-8
//
// [
//   {
//     "open_id": "https://login.staging.ubuntu.com/+id/open_id",
//     "snap_id": "8nzc1x4iim2xj1g2ul64",
//     "refundable_until": "2015-07-15 18:46:21",
//     "state": "Complete"
//   },
//   {
//     "open_id": "https://login.staging.ubuntu.com/+id/open_id",
//     "snap_id": "8nzc1x4iim2xj1g2ul64",
//     "item_sku": "item-1-sku",
//     "purchase_id": "1",
//     "refundable_until": null,
//     "state": "Complete"
//   },
//   {
//     "open_id": "https://login.staging.ubuntu.com/+id/open_id",
//     "snap_id": "12jdhg1j2dgj12dgk1jh",
//     "refundable_until": "2015-07-17 11:33:29",
//     "state": "Complete"
//   }
// ]
type purchase struct {
	OpenID          string `json:"open_id"`
	SnapID          string `json:"snap_id"`
	RefundableUntil string `json:"refundable_until"`
	State           string `json:"state"`
	ItemSKU         string `json:"item_sku,omitempty"`
	PurchaseID      string `json:"purchase_id,omitempty"`
	RedirectTo      string `json:"redirect_to,omitempty"`
}

func (s *Store) getPurchasesFromURL(url *url.URL, channel string, user *auth.UserState) ([]*purchase, error) {
	if user == nil {
		return nil, fmt.Errorf("cannot obtain known purchases from store: no authentication credentials provided")
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    url,
		Accept: halJsonContentType,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var purchases []*purchase

	switch resp.StatusCode {
	case http.StatusOK:
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&purchases); err != nil {
			return nil, fmt.Errorf("cannot decode known purchases from store: %v", err)
		}
	case http.StatusUnauthorized:
		// TODO handle token expiry and refresh
		return nil, ErrInvalidCredentials
	default:
		return nil, respToError(resp, "obtain known purchases from store")
	}

	return purchases, nil
}

func setMustBuy(snaps []*snap.Info) {
	for _, info := range snaps {
		if len(info.Prices) != 0 {
			info.MustBuy = true
		}
	}
}

func hasPriced(snaps []*snap.Info) bool {
	// Search through the list of snaps to see if any are priced
	for _, info := range snaps {
		if len(info.Prices) != 0 {
			return true
		}
	}
	return false
}

// decorateAllPurchases sets the MustBuy property of each snap in the given list according to the user's known purchases.
func (s *Store) decoratePurchases(snaps []*snap.Info, channel string, user *auth.UserState) error {
	// Mark every non-free snap as must buy until we know better.
	setMustBuy(snaps)

	if user == nil {
		return nil
	}

	if !hasPriced(snaps) {
		return nil
	}

	var err error
	var purchasesURL *url.URL

	if len(snaps) == 1 {
		// If we only have a single snap, we should only find the purchases for that snap
		purchasesURL, err = s.purchasesURI.Parse(snaps[0].SnapID + "/")
		if err != nil {
			return err
		}
		q := purchasesURL.Query()
		q.Set("include_item_purchases", "true")
		purchasesURL.RawQuery = q.Encode()
	} else {
		// Inconsistently, global search implies include_item_purchases.
		purchasesURL = s.purchasesURI
	}

	purchases, err := s.getPurchasesFromURL(purchasesURL, channel, user)
	if err != nil {
		return err
	}

	// Group purchases by snap ID.
	purchasesByID := make(map[string][]*purchase)
	for _, purchase := range purchases {
		purchasesByID[purchase.SnapID] = append(purchasesByID[purchase.SnapID], purchase)
	}

	for _, info := range snaps {
		info.MustBuy = mustBuy(info.Prices, purchasesByID[info.SnapID])
	}

	return nil
}

// mustBuy determines if a snap requires a payment, based on if it is non-free and if the user has already bought it
func mustBuy(prices map[string]float64, purchases []*purchase) bool {
	if len(prices) == 0 {
		// If the snap is free, then it doesn't need purchasing
		return false
	}

	// Search through all the purchases for a snap to see if there are any
	// that are for the whole snap, and not an "in-app" purchase.
	for _, purchase := range purchases {
		if purchase.ItemSKU == "" {
			// Purchase is for the whole snap.
			return false
		}
	}

	// The snap is not free, and we couldn't find a purchase for the whole snap.
	return true
}

// Snap returns the snap.Info for the store hosted snap with the given name or an error.
func (s *Store) Snap(name, channel string, devmode bool, revision snap.Revision, user *auth.UserState) (*snap.Info, error) {
	u, err := s.detailsURI.Parse(name)
	if err != nil {
		return nil, err
	}

	query := u.Query()

	if !revision.Unset() {
		query.Set("revision", revision.String())
		query.Set("channel", "") // sidestep the channel map
	} else if channel != "" {
		query.Set("channel", channel)
	}

	// if devmode then don't restrict by confinement as either is fine
	// XXX: what we really want to do is have the store not specify
	//      devmode, and have the business logic wrt what to do with
	//      unwanted devmode further up
	if !devmode {
		query.Set("confinement", string(snap.StrictConfinement))
	}

	u.RawQuery = query.Encode()

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: halJsonContentType,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// check statusCode
	switch resp.StatusCode {
	case http.StatusOK:
		// OK
	case http.StatusNotFound:
		return nil, ErrSnapNotFound
	default:
		msg := fmt.Sprintf("get details for snap %q in channel %q", name, channel)
		return nil, respToError(resp, msg)
	}

	// and decode json
	var remote snapDetails
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&remote); err != nil {
		return nil, err
	}

	info := infoFromRemote(remote)

	err = s.decoratePurchases([]*snap.Info{info}, channel, user)
	if err != nil {
		logger.Noticef("cannot get user purchases: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return info, nil
}

// A Search is what you do in order to Find something
type Search struct {
	Query   string
	Private bool
	Prefix  bool
}

// Find finds  (installable) snaps from the store, matching the
// given Search.
func (s *Store) Find(search *Search, user *auth.UserState) ([]*snap.Info, error) {
	searchTerm := search.Query

	if search.Private && user == nil {
		return nil, ErrUnauthenticated
	}

	searchTerm = strings.TrimSpace(searchTerm)

	if searchTerm == "" {
		return nil, ErrEmptyQuery
	}

	// these characters might have special meaning on the search
	// server, and don't form part of a reasonable search, so
	// abort if they're included.
	//
	// "-" might also be special on the server, but it's also a
	// valid part of a package name, so we let it pass
	if strings.ContainsAny(searchTerm, `+=&|><!(){}[]^"~*?:\/`) {
		return nil, ErrBadQuery
	}

	u := *s.searchURI // make a copy, so we can mutate it
	q := u.Query()

	if search.Private {
		if search.Prefix {
			// The store only supports "fuzzy" search for private snaps.
			// See http://search.apps.ubuntu.com/docs/
			return nil, ErrBadQuery
		}

		q.Set("private", "true")
	}

	if search.Prefix {
		q.Set("name", searchTerm)
	} else {
		q.Set("q", searchTerm)
	}

	q.Set("confinement", "strict")
	u.RawQuery = q.Encode()

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    &u,
		Accept: halJsonContentType,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "search")
	}

	if ct := resp.Header.Get("Content-Type"); ct != halJsonContentType {
		return nil, fmt.Errorf("received an unexpected content type (%q) when trying to search via %q", ct, resp.Request.URL)
	}

	var searchData searchResults

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&searchData); err != nil {
		return nil, fmt.Errorf("cannot decode reply (got %v) when trying to search via %q", err, resp.Request.URL)
	}

	snaps := make([]*snap.Info, len(searchData.Payload.Packages))
	for i, pkg := range searchData.Payload.Packages {
		snaps[i] = infoFromRemote(pkg)
	}

	err = s.decoratePurchases(snaps, "", user)
	if err != nil {
		logger.Noticef("cannot get user purchases: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return snaps, nil
}

// RefreshCandidate contains information for the store about the currently
// installed snap so that the store can decide what update we should see
type RefreshCandidate struct {
	SnapID   string
	Revision snap.Revision
	Epoch    string
	DevMode  bool
	Block    []snap.Revision

	// the desired channel
	Channel string
}

// the exact bits that we need to send to the store
type currentSnapJson struct {
	SnapID   string `json:"snap_id"`
	Channel  string `json:"channel"`
	Revision int    `json:"revision,omitempty"`
	Epoch    string `json:"epoch"`

	// The store expects a "confinement" value {"strict", "devmode"}.
	// We map this accordingly from our devmode bool, we do not
	// use the value of the current snap as we are interested in the
	// users intention, not the actual value of the snap itself.
	Confinement snap.ConfinementType `json:"confinement"`
}

type metadataWrapper struct {
	Snaps  []currentSnapJson `json:"snaps"`
	Fields []string          `json:"fields"`
}

// ListRefresh returns the available updates for a list of snap identified by fullname with channel.
func (s *Store) ListRefresh(installed []*RefreshCandidate, user *auth.UserState) (snaps []*snap.Info, err error) {

	candidateMap := map[string]*RefreshCandidate{}
	currentSnaps := make([]currentSnapJson, 0, len(installed))
	for _, cs := range installed {
		revision := cs.Revision.N
		if !cs.Revision.Store() {
			revision = 0
		}
		// the store gets confused if we send snaps without a snapid
		// (like local ones)
		if cs.SnapID == "" {
			continue
		}

		confinement := snap.StrictConfinement
		if cs.DevMode {
			confinement = snap.DevmodeConfinement
		}

		currentSnaps = append(currentSnaps, currentSnapJson{
			SnapID:      cs.SnapID,
			Channel:     cs.Channel,
			Confinement: confinement,
			Epoch:       cs.Epoch,
			Revision:    revision,
		})
		candidateMap[cs.SnapID] = cs
	}

	// build input for the updates endpoint
	jsonData, err := json.Marshal(metadataWrapper{
		Snaps:  currentSnaps,
		Fields: s.detailFields,
	})
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         s.bulkURI,
		Accept:      halJsonContentType,
		ContentType: "application/json",
		Data:        jsonData,
	}

	if useDeltas() {
		logger.Debugf("Deltas enabled. Adding header X-Ubuntu-Delta-Formats: %v", s.deltaFormat)
		reqOptions.ExtraHeaders = map[string]string{
			"X-Ubuntu-Delta-Formats": s.deltaFormat,
		}
	}

	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, respToError(resp, "query the store for updates")
	}

	var updateData searchResults
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&updateData); err != nil {
		return nil, err
	}

	res := make([]*snap.Info, 0, len(updateData.Payload.Packages))
	for _, rsnap := range updateData.Payload.Packages {
		rrev := snap.R(rsnap.Revision)
		cand := candidateMap[rsnap.SnapID]

		// the store also gives us identical revisions, filter those
		// out, we are not interested
		if rrev == cand.Revision {
			continue
		}
		// do not upgade to a version we rolledback back from
		if findRev(rrev, cand.Block) {
			continue
		}
		res = append(res, infoFromRemote(rsnap))
	}

	s.extractSuggestedCurrency(resp)

	return res, nil
}

func findRev(needle snap.Revision, haystack []snap.Revision) bool {
	for _, r := range haystack {
		if needle == r {
			return true
		}
	}
	return false
}

// Download downloads the snap addressed by download info and returns its
// filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *Store) Download(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (path string, err error) {

	if useDeltas() {
		logger.Debugf("Available deltas returned by store: %v", downloadInfo.Deltas)
	}
	if useDeltas() && len(downloadInfo.Deltas) == 1 {
		snapPath, err := s.downloadAndApplyDelta(name, downloadInfo, pbar, user)
		if err == nil {
			return snapPath, nil
		}
		// We revert to normal downloads if there is any error.
		logger.Noticef("Cannot download or apply deltas for %s: %v", name, err)
	}

	w, err := ioutil.TempFile("", name)
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

	url := downloadInfo.AnonDownloadURL
	if url == "" || user != nil {
		url = downloadInfo.DownloadURL
	}

	if err := download(name, url, user, s, w, pbar); err != nil {
		return "", err
	}

	return w.Name(), w.Sync()
}

// 3 pₙ₊₁ ≥ 5 pₙ; last entry should be 0 -- the sleep is done at the end of the loop
var downloadBackoffs = []int{113, 191, 331, 557, 929, 0}

// download writes an http.Request showing a progress.Meter
var download = func(name, downloadURL string, user *auth.UserState, s *Store, w io.Writer, pbar progress.Meter) error {
	storeURL, err := url.Parse(downloadURL)
	if err != nil {
		return err
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    storeURL,
	}

	var resp *http.Response
	for _, n := range downloadBackoffs {
		// we do *not* want to reuse the client between iterations in
		// this case as it will have internal state (e.g. cached
		// connections) that led us to an error (the default client is
		// documented as not reusing the transport unless the body is
		// read to EOF and closed, so this is a belt-and-braces thing).
		r, err := s.doRequest(&http.Client{}, reqOptions, user)
		if err != nil {
			return err
		}
		defer r.Body.Close()

		resp = r

		if r.StatusCode != 500 {
			break
		}
		time.Sleep(time.Duration(n) * time.Millisecond)
	}
	if resp.StatusCode != 200 {
		return &ErrDownload{Code: resp.StatusCode, URL: resp.Request.URL}
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

// downloadDelta downloads the delta for the preferred format, returning the path.
func (s *Store) downloadDelta(name string, downloadDir string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (string, error) {

	if len(downloadInfo.Deltas) != 1 {
		return "", errors.New("store returned more than one download delta")
	}

	deltaInfo := downloadInfo.Deltas[0]

	if deltaInfo.Format != s.deltaFormat {
		return "", fmt.Errorf("store returned a download delta with the wrong format (%q instead of the configured %s format)", deltaInfo.Format, s.deltaFormat)
	}

	deltaName := fmt.Sprintf("%s_%d_%d_delta.%s", name, deltaInfo.FromRevision, deltaInfo.ToRevision, deltaInfo.Format)

	w, err := os.Create(path.Join(downloadDir, deltaName))
	if err != nil {
		return "", err
	}
	deltaPath := w.Name()
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(w.Name())
			deltaPath = ""
		}
	}()

	url := deltaInfo.AnonDownloadURL
	if url == "" || user != nil {
		url = deltaInfo.DownloadURL
	}

	err = download(deltaName, url, user, s, w, pbar)
	if err != nil {
		return "", err
	}

	// TODO: Check sha3_384
	return deltaPath, nil
}

// applyDelta generates a target snap from a previously downloaded snap and a downloaded delta.
var applyDelta = func(name string, deltaPath string, deltaInfo *snap.DeltaInfo) (string, error) {
	snapBase := fmt.Sprintf("%s_%d.snap", name, deltaInfo.FromRevision)
	snapPath := filepath.Join(dirs.SnapBlobDir, snapBase)

	if !osutil.FileExists(snapPath) {
		return "", fmt.Errorf("snap %q revision %d not found at %s", name, deltaInfo.FromRevision, snapPath)
	}

	if deltaInfo.Format != "xdelta" {
		return "", fmt.Errorf("unsupported delta format %q. Currently only \"xdelta\" format is supported", deltaInfo.Format)
	}

	targetSnapName := fmt.Sprintf("%s_%d_patched_from_%d.snap", name, deltaInfo.ToRevision, deltaInfo.FromRevision)

	// Create a temporary file only to get the unique path.
	tmpfile, err := ioutil.TempFile("", targetSnapName)
	if err != nil {
		return "", err
	}
	targetSnapPath := tmpfile.Name()
	if err = tmpfile.Close(); err != nil {
		return "", err
	}

	xdeltaArgs := []string{"patch", deltaPath, snapPath, targetSnapPath}
	cmd := exec.Command("xdelta", xdeltaArgs...)

	if err = cmd.Run(); err != nil {
		os.Remove(targetSnapPath)
		return "", err
	}

	return targetSnapPath, nil
}

// downloadAndApplyDelta downloads and then applies the delta to the current snap.
func (s *Store) downloadAndApplyDelta(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (path string, err error) {
	deltaInfo := &downloadInfo.Deltas[0]
	workingDir, err := ioutil.TempDir("", name+"-deltas")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workingDir)

	deltaPath, err := s.downloadDelta(name, workingDir, downloadInfo, pbar, user)
	if err != nil {
		return "", err
	}

	logger.Debugf("Successfully downloaded delta for %q at %s", name, deltaPath)
	snapPath, err := applyDelta(name, deltaPath, deltaInfo)
	if err != nil {
		return "", err
	}

	logger.Debugf("Successfully applied delta for %q at %s. Returning %s instead of full download and saving %d bytes.", name, deltaPath, snapPath, downloadInfo.Size-deltaInfo.Size)
	return snapPath, nil
}

type assertionSvcError struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Assertion retrivies the assertion for the given type and primary key.
func (s *Store) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	url, err := s.assertionsURI.Parse(path.Join(assertType.Name, path.Join(primaryKey...)))
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    url,
		Accept: asserts.MediaType,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		contentType := resp.Header.Get("Content-Type")
		if contentType == "application/json" || contentType == "application/problem+json" {
			var svcErr assertionSvcError
			dec := json.NewDecoder(resp.Body)
			if err := dec.Decode(&svcErr); err != nil {
				return nil, fmt.Errorf("cannot decode assertion service error with HTTP status code %d: %v", resp.StatusCode, err)
			}
			if svcErr.Status == 404 {
				return nil, &AssertionNotFoundError{&asserts.Ref{Type: assertType, PrimaryKey: primaryKey}}
			}
			return nil, fmt.Errorf("assertion service error: [%s] %q", svcErr.Title, svcErr.Detail)
		}
		return nil, respToError(resp, "fetch assertion")
	}

	// and decode assertion
	dec := asserts.NewDecoder(resp.Body)
	return dec.Decode()
}

// SuggestedCurrency retrieves the cached value for the store's suggested currency
func (s *Store) SuggestedCurrency() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.suggestedCurrency == "" {
		return "USD"
	}
	return s.suggestedCurrency
}

// BuyOptions specifies parameters for store purchases.
type BuyOptions struct {
	// Required
	SnapID   string  `json:"snap-id"`
	SnapName string  `json:"snap-name"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"` // ISO 4217 code as string

	// Optional
	BackendID string `json:"backend-id"` // e.g. "credit_card", "paypal"
	MethodID  int    `json:"method-id"`  // e.g. a particular credit card or paypal account
}

// BuyResult holds information required to complete the purchase when state
// is "InProgress", in which case it requires user interaction to complete.
type BuyResult struct {
	State      string `json:"state,omitempty"`
	RedirectTo string `json:"redirect-to,omitempty"`
	PartnerID  string `json:"partner-id,omitempty"`
}

// purchaseInstruction holds data sent to the store for purchases.
// X-Device-Id and X-Partner-Id (e.g. "bq") may be sent as headers.
type purchaseInstruction struct {
	SnapID    string  `json:"snap_id"`
	ItemSKU   string  `json:"item_sku,omitempty"`
	Amount    float64 `json:"amount,omitempty"`
	Currency  string  `json:"currency,omitempty"`
	BackendID string  `json:"backend_id,omitempty"`
	MethodID  int     `json:"method_id,omitempty"`
}

type buyError struct {
	ErrorMessage string `json:"error_message"`
}

type storeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *storeError) Error() string {
	return s.Message
}

type storeErrors struct {
	Errors []*storeError `json:"error_list"`
}

func (s *storeErrors) Error() string {
	if len(s.Errors) == 0 {
		return "internal error: empty store error used as an actual error"
	}
	return "store reported an error: " + s.Errors[0].Error()
}

func buyOptionError(options *BuyOptions, message string) (*BuyResult, error) {
	identifier := ""
	if options.SnapName != "" {
		identifier = fmt.Sprintf(" %q", options.SnapName)
	} else if options.SnapID != "" {
		identifier = fmt.Sprintf(" %q", options.SnapID)
	}

	return nil, fmt.Errorf("cannot buy snap%s: %s", identifier, message)
}

// Buy sends a purchase request for the specified snap.
// Returns the state of the purchase: Complete, Cancelled, InProgress or Pending.
func (s *Store) Buy(options *BuyOptions, user *auth.UserState) (*BuyResult, error) {
	if options.SnapID == "" {
		return buyOptionError(options, "snap ID missing")
	}
	if options.SnapName == "" {
		return buyOptionError(options, "snap name missing")
	}
	if options.Price <= 0 {
		return buyOptionError(options, "invalid expected price")
	}
	if options.Currency == "" {
		return buyOptionError(options, "currency missing")
	}
	if user == nil {
		return buyOptionError(options, "authentication credentials missing")
	}

	instruction := purchaseInstruction{
		SnapID:    options.SnapID,
		Amount:    options.Price,
		Currency:  options.Currency,
		BackendID: options.BackendID,
		MethodID:  options.MethodID,
	}

	jsonData, err := json.Marshal(instruction)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         s.purchasesURI,
		Accept:      halJsonContentType,
		ContentType: "application/json",
		Data:        jsonData,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// user already purchased or purchase successful
		var purchaseDetails purchase
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&purchaseDetails); err != nil {
			return nil, err
		}

		if purchaseDetails.State == "Cancelled" {
			return nil, fmt.Errorf("cannot buy snap %q: payment cancelled", options.SnapName)
		}

		redirectTo := ""
		if purchaseDetails.RedirectTo != "" {
			redirectTo = fmt.Sprintf("%s://%s%s", s.purchasesURI.Scheme, s.purchasesURI.Host, purchaseDetails.RedirectTo)
		}

		return &BuyResult{
			State:      purchaseDetails.State,
			RedirectTo: redirectTo,
			PartnerID:  resp.Header.Get("X-Partner-Id"),
		}, nil
	case http.StatusBadRequest:
		// Invalid price was specified, etc.
		var errorInfo buyError
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&errorInfo); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("cannot buy snap %q: bad request: %s", options.SnapName, errorInfo.ErrorMessage)
	case http.StatusNotFound:
		// Likely because snap ID doesn't exist.
		return nil, fmt.Errorf("cannot buy snap %q: server says not found (snap got removed?)", options.SnapName)
	case http.StatusUnauthorized:
		// TODO handle token expiry and refresh
		return nil, ErrInvalidCredentials
	default:
		var errorInfo buyError
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&errorInfo); err != nil {
			return nil, err
		}
		details := ""
		if errorInfo.ErrorMessage != "" {
			details = ": " + errorInfo.ErrorMessage
		}
		return nil, respToError(resp, fmt.Sprintf("buy snap %q%s", options.SnapName, details))
	}
}

type storePaymentBackend struct {
	Choices     []*storePaymentMethod `json:"choices"`
	Description string                `json:"description"`
	ID          string                `json:"id"`
	Preferred   bool                  `json:"preferred"`
}

type storePaymentMethod struct {
	Currencies          []string `json:"currencies"`
	Description         string   `json:"description"`
	ID                  int      `json:"id"`
	Preferred           bool     `json:"preferred"`
	RequiresInteraction bool     `json:"requires_interaction"`
}

type PaymentMethod struct {
	BackendID           string   `json:"backend-id"`
	Currencies          []string `json:"currencies"`
	Description         string   `json:"description"`
	ID                  int      `json:"id"`
	Preferred           bool     `json:"preferred"`
	RequiresInteraction bool     `json:"requires-interaction"`
}

type PaymentInformation struct {
	AllowsAutomaticPayment bool             `json:"allows-automatic-payment"`
	Methods                []*PaymentMethod `json:"methods"`
}

type storeCustomer struct {
	LatestTOSDate     string `json:"latest_tos_date"`
	AcceptedTOSDate   string `json:"accepted_tos_date"`
	LatestTOSAccepted bool   `json:"latest_tos_accepted"`
	HasPaymentMethod  bool   `json:"has_payment_method"`
}

// ReadyToBuy returns nil if the user's account has accepted T&Cs and has a payment method registered, and an error otherwise
func (s *Store) ReadyToBuy(user *auth.UserState) error {
	if user == nil {
		return ErrInvalidCredentials
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    s.customersMeURI,
		Accept: jsonContentType,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var customer storeCustomer
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&customer); err != nil {
			return err
		}
		if !customer.LatestTOSAccepted {
			return ErrTOSNotAccepted
		}
		if !customer.HasPaymentMethod {
			return ErrNoPaymentMethods
		}
		return nil
	case http.StatusNotFound:
		// Likely because user has no account registered on the pay server
		return fmt.Errorf("cannot get customer details: server says no account exists")
	case http.StatusUnauthorized:
		return ErrInvalidCredentials
	default:
		var errors storeErrors
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&errors); err != nil {
			return err
		}
		if len(errors.Errors) == 0 {
			return fmt.Errorf("cannot get customer details: unexpected HTTP code %d", resp.StatusCode)
		}
		return &errors
	}
}

// PaymentMethods gets a list of the individual payment methods the user has registerd against their Ubuntu One account
// TODO Remove once the CLI is using the new /buy/ready endpoint
func (s *Store) PaymentMethods(user *auth.UserState) (*PaymentInformation, error) {
	if user == nil {
		return nil, ErrInvalidCredentials
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    s.paymentMethodsURI,
		Accept: halJsonContentType,
	}
	resp, err := s.doRequest(s.client, reqOptions, user)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var paymentBackends []*storePaymentBackend
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&paymentBackends); err != nil {
			return nil, err
		}

		paymentMethods := &PaymentInformation{
			AllowsAutomaticPayment: false,
			Methods:                make([]*PaymentMethod, 0),
		}

		// Unroll nested structure into a simple list of PaymentMethods
		for _, backend := range paymentBackends {

			if backend.Preferred {
				paymentMethods.AllowsAutomaticPayment = true
			}

			for _, method := range backend.Choices {
				paymentMethods.Methods = append(paymentMethods.Methods, &PaymentMethod{
					BackendID:           backend.ID,
					Currencies:          method.Currencies,
					Description:         method.Description,
					ID:                  method.ID,
					Preferred:           method.Preferred,
					RequiresInteraction: method.RequiresInteraction,
				})
			}
		}

		return paymentMethods, nil
	case http.StatusUnauthorized:
		return nil, ErrInvalidCredentials
	default:
		var errorInfo buyError
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&errorInfo); err != nil {
			return nil, err
		}
		details := ""
		if errorInfo.ErrorMessage != "" {
			details = ": " + errorInfo.ErrorMessage
		}
		return nil, fmt.Errorf("cannot get payment methods: unexpected HTTP code %d%s", resp.StatusCode, details)
	}
}
