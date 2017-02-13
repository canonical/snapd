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
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"

	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
	"gopkg.in/retry.v1"
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
	info.PublisherID = d.DeveloperID
	info.Publisher = d.Developer
	info.Channel = d.Channel
	info.Sha3_384 = d.DownloadSha3_384
	info.Size = d.DownloadSize
	info.IconURL = d.IconURL
	info.AnonDownloadURL = d.AnonDownloadURL
	info.DownloadURL = d.DownloadURL
	info.Prices = d.Prices
	info.Private = d.Private
	info.Confinement = snap.ConfinementType(d.Confinement)
	info.Contact = d.Contact

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
	// FIXME: once the store sends "contact" for everything, remove
	//        the "SupportURL" part of the if
	if info.Contact == "" {
		info.Contact = d.SupportURL
	}

	return info
}

// Config represents the configuration to access the snap store
type Config struct {
	SearchURI      *url.URL
	DetailsURI     *url.URL
	BulkURI        *url.URL
	AssertionsURI  *url.URL
	OrdersURI      *url.URL
	CustomersMeURI *url.URL
	SectionsURI    *url.URL

	// StoreID is the store id used if we can't get one through the AuthContext.
	StoreID string

	Architecture string
	Series       string

	DetailFields []string
	DeltaFormat  string
}

// Store represents the ubuntu snap store
type Store struct {
	searchURI      *url.URL
	detailsURI     *url.URL
	bulkURI        *url.URL
	assertionsURI  *url.URL
	ordersURI      *url.URL
	customersMeURI *url.URL
	sectionsURI    *url.URL

	architecture string
	series       string

	noCDN bool

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

// Deltas enabled by default on classic, but allow opting in or out on both classic and core.
func useDeltas() bool {
	deltasDefault := release.OnClassic
	// only xdelta3 is supported for now, so check the binary exists here
	// TODO: have a per-format checker instead
	return osutil.GetenvBool("SNAPD_USE_DELTAS_EXPERIMENTAL", deltasDefault) && osutil.ExecutableExists("xdelta3")
}

func useStaging() bool {
	return osutil.GetenvBool("SNAPPY_USE_STAGING_STORE")
}

func cpiURL() string {
	// FIXME: this will become a store-url assertion
	if u := os.Getenv("SNAPPY_FORCE_CPI_URL"); u != "" {
		return u
	}
	if useStaging() {
		return "https://search.apps.staging.ubuntu.com/api/v1/"
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
	if u := os.Getenv("SNAPPY_FORCE_SSO_URL"); u != "" {
		return u
	}
	return "https://" + authLocation() + "/api/v2"
}

func assertsURL() string {
	if u := os.Getenv("SNAPPY_FORCE_SAS_URL"); u != "" {
		return u
	}
	if useStaging() {
		return "https://assertions.staging.ubuntu.com/v1/"
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

	defaultConfig.OrdersURI, err = url.Parse(myappsURL() + "purchases/v1/orders")
	if err != nil {
		panic(err)
	}

	defaultConfig.CustomersMeURI, err = url.Parse(myappsURL() + "purchases/v1/customers/me")
	if err != nil {
		panic(err)
	}

	defaultConfig.SectionsURI, err = storeBaseURI.Parse("snaps/sections")
	if err != nil {
		panic(err)
	}
}

type searchResults struct {
	Payload struct {
		Packages []snapDetails `json:"clickindex:package"`
	} `json:"_embedded"`
}

type sectionResults struct {
	Payload struct {
		Sections []struct{ Name string } `json:"clickindex:sections"`
	} `json:"_embedded"`
}

// The fields we are interested in
var detailFields = getStructFields(snapDetails{})

// The fields we are interested in for snap.ChannelSnapInfos
var channelSnapInfoFields = getStructFields(channelSnapInfoDetails{})

// The default delta format if not configured.
var defaultSupportedDeltaFormat = "xdelta3"

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

	var sectionsURI *url.URL
	if cfg.SectionsURI != nil {
		sectionsURI = cfg.SectionsURI
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
		searchURI:       searchURI,
		detailsURI:      detailsURI,
		bulkURI:         cfg.BulkURI,
		assertionsURI:   cfg.AssertionsURI,
		ordersURI:       cfg.OrdersURI,
		customersMeURI:  cfg.CustomersMeURI,
		sectionsURI:     sectionsURI,
		series:          series,
		architecture:    architecture,
		noCDN:           osutil.GetenvBool("SNAPPY_STORE_NO_CDN"),
		fallbackStoreID: cfg.StoreID,
		detailFields:    fields,
		authContext:     authContext,
		deltaFormat:     deltaFormat,

		client: httputil.NewHTTPClient(&httputil.ClientOpts{
			Timeout:    10 * time.Second,
			MayLogBody: true,
		}),
	}
}

// LoginUser logs user in the store and returns the authentication macaroons.
func LoginUser(username, password, otp string) (string, string, error) {
	macaroon, err := requestStoreMacaroon()
	if err != nil {
		return "", "", err
	}
	deserializedMacaroon, err := auth.MacaroonDeserialize(macaroon)
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

// hasStoreAuth returns true if given user has store macaroons setup
func hasStoreAuth(user *auth.UserState) bool {
	return user != nil && user.StoreMacaroon != ""
}

// authenticateUser will add the store expected Macaroon Authorization header for user
func authenticateUser(r *http.Request, user *auth.UserState) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `Macaroon root="%s"`, user.StoreMacaroon)

	// deserialize root macaroon (we need its signature to do the discharge binding)
	root, err := auth.MacaroonDeserialize(user.StoreMacaroon)
	if err != nil {
		logger.Debugf("cannot deserialize root macaroon: %v", err)
		return
	}

	for _, d := range user.StoreDischarges {
		// prepare discharge for request
		discharge, err := auth.MacaroonDeserialize(d)
		if err != nil {
			logger.Debugf("cannot deserialize discharge macaroon: %v", err)
			return
		}
		discharge.Bind(root.Signature())

		serializedDischarge, err := auth.MacaroonSerialize(discharge)
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
		discharge, err := auth.MacaroonDeserialize(d)
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

func cancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// retryRequestDecodeJSON calls retryRequest and decodes the response into either success or failure.
func (s *Store) retryRequestDecodeJSON(ctx context.Context, client *http.Client, reqOptions *requestOptions, user *auth.UserState, success interface{}, failure interface{}) (resp *http.Response, err error) {
	return s.retryRequest(ctx, client, reqOptions, user, func(ok bool, resp *http.Response) error {
		result := success
		if !ok {
			result = failure
		}
		if result != nil {
			return json.NewDecoder(resp.Body).Decode(result)
		}
		return nil
	})
}

// retryRequest calls doRequest and decodes the response in a retry loop.
func (s *Store) retryRequest(ctx context.Context, client *http.Client, reqOptions *requestOptions, user *auth.UserState, decode func(ok bool, resp *http.Response) error) (resp *http.Response, err error) {
	var attempt *retry.Attempt
	startTime := time.Now()
	for attempt = retry.Start(defaultRetryStrategy, nil); attempt.Next(); {
		maybeLogRetryAttempt(reqOptions.URL.String(), attempt, startTime)
		if cancelled(ctx) {
			return nil, ctx.Err()
		}

		resp, err = s.doRequest(ctx, client, reqOptions, user)
		if err != nil {
			if shouldRetryError(attempt, err) {
				continue
			}
			break
		}

		if shouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		} else {
			ok := (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)
			// always decode on success; decode failures only if body is not empty
			if !ok && resp.ContentLength == 0 {
				resp.Body.Close()
				break
			}
			err = decode(ok, resp)
			resp.Body.Close()
			if err != nil {
				if shouldRetryError(attempt, err) {
					continue
				} else {
					return nil, err
				}
			}
		}
		// break out from retry loop
		break
	}
	maybeLogRetrySummary(startTime, reqOptions.URL.String(), attempt, resp, err)

	return resp, err
}

// doRequest does an authenticated request to the store handling a potential macaroon refresh required if needed
func (s *Store) doRequest(ctx context.Context, client *http.Client, reqOptions *requestOptions, user *auth.UserState) (*http.Response, error) {
	req, err := s.newRequest(reqOptions, user)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	if ctx != nil {
		resp, err = ctxhttp.Do(ctx, client, req)
	} else {
		resp, err = client.Do(req)
	}
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
			return s.doRequest(ctx, client, reqOptions, user)
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

	// only set user authentication if user logged in to the store
	if hasStoreAuth(user) {
		authenticateUser(req, user)
	}

	req.Header.Set("User-Agent", httputil.UserAgent())
	req.Header.Set("Accept", reqOptions.Accept)
	req.Header.Set("X-Ubuntu-Architecture", s.architecture)
	req.Header.Set("X-Ubuntu-Series", s.series)
	req.Header.Set("X-Ubuntu-Classic", strconv.FormatBool(release.OnClassic))
	req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)
	req.Header.Set("X-Ubuntu-No-CDN", strconv.FormatBool(s.noCDN))

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

// ordersResult encapsulates the order data sent to us from the software center agent.
//
// {
//   "orders": [
//     {
//       "snap_id": "abcd1234efgh5678ijkl9012",
//       "currency": "USD",
//       "amount": "2.99",
//       "state": "Complete",
//       "refundable_until": null,
//       "purchase_date": "2016-09-20T15:00:00+00:00"
//     },
//     {
//       "snap_id": "abcd1234efgh5678ijkl9012",
//       "currency": null,
//       "amount": null,
//       "state": "Complete",
//       "refundable_until": null,
//       "purchase_date": "2016-09-20T15:00:00+00:00"
//     }
//   ]
// }
type ordersResult struct {
	Orders []*order `json:"orders"`
}

type order struct {
	SnapID          string `json:"snap_id"`
	Currency        string `json:"currency"`
	Amount          string `json:"amount"`
	State           string `json:"state"`
	RefundableUntil string `json:"refundable_until"`
	PurchaseDate    string `json:"purchase_date"`
}

// decorateOrders sets the MustBuy property of each snap in the given list according to the user's known orders.
func (s *Store) decorateOrders(snaps []*snap.Info, channel string, user *auth.UserState) error {
	// Mark every non-free snap as must buy until we know better.
	hasPriced := false
	for _, info := range snaps {
		if len(info.Prices) != 0 {
			info.MustBuy = true
			hasPriced = true
		}
	}

	if user == nil {
		return nil
	}

	if !hasPriced {
		return nil
	}

	var err error

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    s.ordersURI,
		Accept: jsonContentType,
	}
	var result ordersResult
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &result, nil)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// TODO handle token expiry and refresh
		return ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return respToError(resp, "obtain known orders from store")
	}

	// Make a map of the IDs of bought snaps
	bought := make(map[string]bool)
	for _, order := range result.Orders {
		bought[order.SnapID] = true
	}

	for _, info := range snaps {
		info.MustBuy = mustBuy(info.Prices, bought[info.SnapID])
	}

	return nil
}

// mustBuy determines if a snap requires a payment, based on if it is non-free and if the user has already bought it
func mustBuy(prices map[string]float64, bought bool) bool {
	if len(prices) == 0 {
		// If the snap is free, then it doesn't need buying
		return false
	}

	return !bought
}

// fakeChannels is a stopgap method of getting pseudo-channels until
// the details endpoint provides the real thing for us. The main
// difference between this pseudo one and the real thing is that a
// channel can be closed, and we'll be oblivious to it.
func (s *Store) fakeChannels(snapID string, user *auth.UserState) (map[string]*snap.ChannelSnapInfo, error) {
	snaps := make([]currentSnapJson, 4)
	for i, channel := range []string{"stable", "candidate", "beta", "edge"} {
		snaps[i] = currentSnapJson{
			SnapID:  snapID,
			Channel: channel,
			// revision, confinement, epoch purposely left empty
		}
	}
	jsonData, err := json.Marshal(metadataWrapper{
		Snaps:  snaps,
		Fields: channelSnapInfoFields,
	})
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         s.bulkURI,
		Accept:      halJsonContentType,
		ContentType: jsonContentType,
		Data:        jsonData,
	}

	var results struct {
		Payload struct {
			ChannelSnapInfoDetails []*channelSnapInfoDetails `json:"clickindex:package"`
		} `json:"_embedded"`
	}

	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &results, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, respToError(resp, "query the store for channel information")
	}

	channelInfos := make(map[string]*snap.ChannelSnapInfo, 4)
	for _, item := range results.Payload.ChannelSnapInfoDetails {
		channelInfos[item.Channel] = &snap.ChannelSnapInfo{
			Revision:    snap.R(item.Revision),
			Confinement: snap.ConfinementType(item.Confinement),
			Version:     item.Version,
			Channel:     item.Channel,
			Epoch:       item.Epoch,
			Size:        item.DownloadSize,
		}
	}

	return channelInfos, nil
}

// A SnapSpec describes a single snap wanted from SnapInfo
type SnapSpec struct {
	Name     string
	Channel  string
	Revision snap.Revision
}

// SnapInfo returns the snap.Info for the store-hosted snap matching the given spec, or an error.
func (s *Store) SnapInfo(snapSpec SnapSpec, user *auth.UserState) (*snap.Info, error) {
	// get the query before doing Parse, as that overwrites it
	query := s.detailsURI.Query()
	u, err := s.detailsURI.Parse(snapSpec.Name)
	if err != nil {
		return nil, err
	}

	query.Set("channel", snapSpec.Channel)
	if !snapSpec.Revision.Unset() {
		query.Set("revision", snapSpec.Revision.String())
		query.Set("channel", "")
	}

	u.RawQuery = query.Encode()

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: halJsonContentType,
	}

	var remote snapDetails
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &remote, nil)
	if err != nil {
		return nil, err
	}

	// check statusCode
	switch resp.StatusCode {
	case http.StatusOK:
		// OK
	case http.StatusNotFound:
		return nil, ErrSnapNotFound
	default:
		msg := fmt.Sprintf("get details for snap %q in channel %q", snapSpec.Name, snapSpec.Channel)
		return nil, respToError(resp, msg)
	}

	info := infoFromRemote(remote)

	// only get the channels when it makes sense as part of the reply
	if info.SnapID != "" && snapSpec.Channel == "" && snapSpec.Revision.Unset() {
		channels, err := s.fakeChannels(info.SnapID, user)
		if err != nil {
			logger.Noticef("cannot get channels: %v", err)
		} else {
			info.Channels = channels
		}
	}

	err = s.decorateOrders([]*snap.Info{info}, snapSpec.Channel, user)
	if err != nil {
		logger.Noticef("cannot get user orders: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return info, nil
}

// A Search is what you do in order to Find something
type Search struct {
	Query   string
	Section string
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
	if search.Section != "" {
		q.Set("section", search.Section)
	}

	if release.OnClassic {
		q.Set("confinement", "strict,classic")
	} else {
		q.Set("confinement", "strict")
	}
	u.RawQuery = q.Encode()

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    &u,
		Accept: halJsonContentType,
	}

	var searchData searchResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &searchData, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "search")
	}

	if ct := resp.Header.Get("Content-Type"); ct != halJsonContentType {
		return nil, fmt.Errorf("received an unexpected content type (%q) when trying to search via %q", ct, resp.Request.URL)
	}

	snaps := make([]*snap.Info, len(searchData.Payload.Packages))
	for i, pkg := range searchData.Payload.Packages {
		snaps[i] = infoFromRemote(pkg)
	}

	err = s.decorateOrders(snaps, "", user)
	if err != nil {
		logger.Noticef("cannot get user orders: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return snaps, nil
}

// Sections retrieves the list of available store sections.
func (s *Store) Sections(user *auth.UserState) ([]string, error) {
	u := *s.sectionsURI // make a copy, so we can mutate it

	q := u.Query()

	u.RawQuery = q.Encode()

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    &u,
		Accept: halJsonContentType,
	}

	var sectionData sectionResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &sectionData, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "sections")
	}

	if ct := resp.Header.Get("Content-Type"); ct != halJsonContentType {
		return nil, fmt.Errorf("received an unexpected content type (%q) when trying to retrieve the sections via %q", ct, resp.Request.URL)
	}

	var sectionNames []string
	for _, s := range sectionData.Payload.Sections {
		sectionNames = append(sectionNames, s.Name)
	}

	return sectionNames, nil
}

// RefreshCandidate contains information for the store about the currently
// installed snap so that the store can decide what update we should see
type RefreshCandidate struct {
	SnapID   string
	Revision snap.Revision
	Epoch    string
	Block    []snap.Revision

	// the desired channel
	Channel string
}

// the exact bits that we need to send to the store
type currentSnapJson struct {
	SnapID      string `json:"snap_id"`
	Channel     string `json:"channel"`
	Revision    int    `json:"revision,omitempty"`
	Epoch       string `json:"epoch"`
	Confinement string `json:"confinement"`
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

		currentSnaps = append(currentSnaps, currentSnapJson{
			SnapID:   cs.SnapID,
			Channel:  cs.Channel,
			Epoch:    cs.Epoch,
			Revision: revision,
			// confinement purposely left empty
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
		ContentType: jsonContentType,
		Data:        jsonData,
	}

	if useDeltas() {
		logger.Debugf("Deltas enabled. Adding header X-Ubuntu-Delta-Formats: %v", s.deltaFormat)
		reqOptions.ExtraHeaders = map[string]string{
			"X-Ubuntu-Delta-Formats": s.deltaFormat,
		}
	}

	var updateData searchResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &updateData, nil)

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, respToError(resp, "query the store for updates")
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

type HashError struct {
	name           string
	sha3_384       string
	targetSha3_384 string
}

func (e HashError) Error() string {
	return fmt.Sprintf("sha3-384 mismatch after patching %q: got %s but expected %s", e.name, e.sha3_384, e.targetSha3_384)
}

// Download downloads the snap addressed by download info and returns its
// filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *Store) Download(ctx context.Context, name string, targetPath string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}
	if useDeltas() {
		logger.Debugf("Available deltas returned by store: %v", downloadInfo.Deltas)
	}
	if useDeltas() && len(downloadInfo.Deltas) == 1 {
		err := s.downloadAndApplyDelta(name, targetPath, downloadInfo, pbar, user)
		if err == nil {
			return nil
		}
		// We revert to normal downloads if there is any error.
		logger.Noticef("Cannot download or apply deltas for %s: %v", name, err)
	}

	partialPath := targetPath + ".partial"
	w, err := os.OpenFile(partialPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	resume, err := w.Seek(0, os.SEEK_END)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(w.Name())
		}
	}()

	url := downloadInfo.AnonDownloadURL
	if url == "" || hasStoreAuth(user) {
		url = downloadInfo.DownloadURL
	}

	err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, resume, pbar)
	// If sha3 checksum is incorrect and it was a resumed download, retry from scratch.
	// Note that we will retry this way only once.
	if _, ok := err.(HashError); ok && resume > 0 {
		logger.Debugf("Error on resumed download: %v", err.Error())
		err = w.Truncate(0)
		if err != nil {
			return err
		}
		_, err = w.Seek(0, os.SEEK_SET)
		if err != nil {
			return err
		}
		err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, 0, pbar)
	}

	if err != nil {
		return err
	}

	if err := os.Rename(w.Name(), targetPath); err != nil {
		return err
	}

	return w.Sync()
}

// download writes an http.Request showing a progress.Meter
var download = func(ctx context.Context, name, sha3_384, downloadURL string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
	storeURL, err := url.Parse(downloadURL)
	if err != nil {
		return err
	}

	var finalErr error
	startTime := time.Now()
	for attempt := retry.Start(defaultRetryStrategy, nil); attempt.Next(); {
		reqOptions := &requestOptions{
			Method: "GET",
			URL:    storeURL,
		}
		maybeLogRetryAttempt(reqOptions.URL.String(), attempt, startTime)

		h := crypto.SHA3_384.New()

		if resume > 0 {
			reqOptions.ExtraHeaders = map[string]string{
				"Range": fmt.Sprintf("bytes=%d-", resume),
			}
			// seed the sha3 with the already local file
			if _, err := w.Seek(0, os.SEEK_SET); err != nil {
				return err
			}
			n, err := io.Copy(h, w)
			if err != nil {
				return err
			}
			if n != resume {
				return fmt.Errorf("resume offset wrong: %d != %d", resume, n)
			}
		}

		if cancelled(ctx) {
			return fmt.Errorf("The download has been cancelled: %s", ctx.Err())
		}
		var resp *http.Response
		resp, finalErr = s.doRequest(ctx, httputil.NewHTTPClient(nil), reqOptions, user)

		if cancelled(ctx) {
			return fmt.Errorf("The download has been cancelled: %s", ctx.Err())
		}
		if finalErr != nil {
			if shouldRetryError(attempt, finalErr) {
				continue
			}
			break
		}

		if shouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		}

		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK, http.StatusPartialContent:
		case http.StatusUnauthorized:
			return fmt.Errorf("cannot download non-free snap without purchase")
		default:
			return &ErrDownload{Code: resp.StatusCode, URL: resp.Request.URL}
		}

		if pbar == nil {
			pbar = &progress.NullProgress{}
		}
		pbar.Start(name, float64(resp.ContentLength))
		mw := io.MultiWriter(w, h, pbar)
		_, finalErr = io.Copy(mw, resp.Body)
		pbar.Finished()
		if finalErr != nil {
			if shouldRetryError(attempt, finalErr) {
				// error while downloading should resume
				var seekerr error
				resume, seekerr = w.Seek(0, os.SEEK_END)
				if seekerr == nil {
					continue
				}
				// if seek failed, then don't retry end return the original error
			}
			break
		}

		if cancelled(ctx) {
			return fmt.Errorf("The download has been cancelled: %s", ctx.Err())
		}

		actualSha3 := fmt.Sprintf("%x", h.Sum(nil))
		if sha3_384 != "" && sha3_384 != actualSha3 {
			finalErr = HashError{name, actualSha3, sha3_384}
		}
		break
	}
	return finalErr
}

// downloadDelta downloads the delta for the preferred format, returning the path.
func (s *Store) downloadDelta(deltaName string, downloadInfo *snap.DownloadInfo, w io.ReadWriteSeeker, pbar progress.Meter, user *auth.UserState) error {

	if len(downloadInfo.Deltas) != 1 {
		return errors.New("store returned more than one download delta")
	}

	deltaInfo := downloadInfo.Deltas[0]

	if deltaInfo.Format != s.deltaFormat {
		return fmt.Errorf("store returned unsupported delta format %q (only xdelta3 currently)", deltaInfo.Format)
	}

	url := deltaInfo.AnonDownloadURL
	if url == "" || hasStoreAuth(user) {
		url = deltaInfo.DownloadURL
	}

	return download(context.TODO(), deltaName, deltaInfo.Sha3_384, url, user, s, w, 0, pbar)
}

// applyDelta generates a target snap from a previously downloaded snap and a downloaded delta.
var applyDelta = func(name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
	snapBase := fmt.Sprintf("%s_%d.snap", name, deltaInfo.FromRevision)
	snapPath := filepath.Join(dirs.SnapBlobDir, snapBase)

	if !osutil.FileExists(snapPath) {
		return fmt.Errorf("snap %q revision %d not found at %s", name, deltaInfo.FromRevision, snapPath)
	}

	if deltaInfo.Format != "xdelta3" {
		return fmt.Errorf("cannot apply unsupported delta format %q (only xdelta3 currently)", deltaInfo.Format)
	}

	partialTargetPath := targetPath + ".partial"

	xdelta3Args := []string{"-d", "-s", snapPath, deltaPath, partialTargetPath}
	cmd := exec.Command("xdelta3", xdelta3Args...)

	if err := cmd.Run(); err != nil {
		if err := os.Remove(partialTargetPath); err != nil {
			logger.Noticef("failed to remove partial delta target %q: %s", partialTargetPath, err)
		}
		return err
	}

	bsha3_384, _, err := osutil.FileDigest(partialTargetPath, crypto.SHA3_384)
	if err != nil {
		return err
	}
	sha3_384 := fmt.Sprintf("%x", bsha3_384)
	if targetSha3_384 != "" && sha3_384 != targetSha3_384 {
		if err := os.Remove(partialTargetPath); err != nil {
			logger.Noticef("failed to remove partial delta target %q: %s", partialTargetPath, err)
		}
		return HashError{name, sha3_384, targetSha3_384}
	}

	if err := os.Rename(partialTargetPath, targetPath); err != nil {
		return osutil.CopyFile(partialTargetPath, targetPath, 0)
	}

	return nil
}

// downloadAndApplyDelta downloads and then applies the delta to the current snap.
func (s *Store) downloadAndApplyDelta(name, targetPath string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) error {
	deltaInfo := &downloadInfo.Deltas[0]

	deltaPath := fmt.Sprintf("%s.%s-%d-to-%d.partial", targetPath, deltaInfo.Format, deltaInfo.FromRevision, deltaInfo.ToRevision)
	deltaName := fmt.Sprintf(i18n.G("%s (delta)"), name)

	w, err := os.Create(deltaPath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		os.Remove(deltaPath)
	}()

	err = s.downloadDelta(deltaName, downloadInfo, w, pbar, user)
	if err != nil {
		return err
	}

	logger.Debugf("Successfully downloaded delta for %q at %s", name, deltaPath)
	if err := applyDelta(name, deltaPath, deltaInfo, targetPath, downloadInfo.Sha3_384); err != nil {
		return err
	}

	logger.Debugf("Successfully applied delta for %q at %s, saving %d bytes.", name, deltaPath, downloadInfo.Size-deltaInfo.Size)
	return nil
}

type assertionSvcError struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Assertion retrivies the assertion for the given type and primary key.
func (s *Store) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	u, err := s.assertionsURI.Parse(path.Join(assertType.Name, path.Join(primaryKey...)))
	if err != nil {
		return nil, err
	}
	v := url.Values{}
	v.Set("max-format", strconv.Itoa(assertType.MaxSupportedFormat()))
	u.RawQuery = v.Encode()

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: asserts.MediaType,
	}

	var asrt asserts.Assertion
	resp, err := s.retryRequest(context.TODO(), s.client, reqOptions, user, func(ok bool, resp *http.Response) error {
		var e error
		if ok {
			// decode assertion
			dec := asserts.NewDecoder(resp.Body)
			asrt, e = dec.Decode()
		} else {
			contentType := resp.Header.Get("Content-Type")
			if contentType == jsonContentType || contentType == "application/problem+json" {
				var svcErr assertionSvcError
				dec := json.NewDecoder(resp.Body)
				if e = dec.Decode(&svcErr); e != nil {
					return fmt.Errorf("cannot decode assertion service error with HTTP status code %d: %v", resp.StatusCode, e)
				}
				if svcErr.Status == 404 {
					return &AssertionNotFoundError{&asserts.Ref{Type: assertType, PrimaryKey: primaryKey}}
				}
				return fmt.Errorf("assertion service error: [%s] %q", svcErr.Title, svcErr.Detail)
			}
		}
		return e
	})

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "fetch assertion")
	}

	return asrt, err
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

// BuyOptions specifies parameters to buy from the store.
type BuyOptions struct {
	SnapID   string  `json:"snap-id"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"` // ISO 4217 code as string
}

// BuyResult holds the state of a buy attempt.
type BuyResult struct {
	State string `json:"state,omitempty"`
}

// orderInstruction holds data sent to the store for orders.
type orderInstruction struct {
	SnapID   string `json:"snap_id"`
	Amount   string `json:"amount,omitempty"`
	Currency string `json:"currency,omitempty"`
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

func buyOptionError(message string) (*BuyResult, error) {
	return nil, fmt.Errorf("cannot buy snap: %s", message)
}

// Buy sends a buy request for the specified snap.
// Returns the state of the order: Complete, Cancelled.
func (s *Store) Buy(options *BuyOptions, user *auth.UserState) (*BuyResult, error) {
	if options.SnapID == "" {
		return buyOptionError("snap ID missing")
	}
	if options.Price <= 0 {
		return buyOptionError("invalid expected price")
	}
	if options.Currency == "" {
		return buyOptionError("currency missing")
	}
	if user == nil {
		return nil, ErrUnauthenticated
	}

	// FIXME Would really rather not to do this, and have the same meaningful errors from the POST to order.
	err := s.ReadyToBuy(user)
	if err != nil {
		return nil, err
	}

	instruction := orderInstruction{
		SnapID:   options.SnapID,
		Amount:   fmt.Sprintf("%.2f", options.Price),
		Currency: options.Currency,
	}

	jsonData, err := json.Marshal(instruction)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         s.ordersURI,
		Accept:      jsonContentType,
		ContentType: jsonContentType,
		Data:        jsonData,
	}

	var orderDetails order
	var errorInfo storeErrors
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &orderDetails, &errorInfo)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// user already ordered or order successful
		if orderDetails.State == "Cancelled" {
			return buyOptionError("payment cancelled")
		}

		return &BuyResult{
			State: orderDetails.State,
		}, nil
	case http.StatusBadRequest:
		// Invalid price was specified, etc.
		return buyOptionError(fmt.Sprintf("bad request: %v", errorInfo.Error()))
	case http.StatusNotFound:
		// Likely because snap ID doesn't exist.
		return buyOptionError("server says not found (snap got removed?)")
	case http.StatusPaymentRequired:
		// Payment failed for some reason.
		return nil, ErrPaymentDeclined
	case http.StatusUnauthorized:
		// TODO handle token expiry and refresh
		return nil, ErrInvalidCredentials
	default:
		return nil, respToError(resp, fmt.Sprintf("buy snap: %v", errorInfo))
	}
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
		return ErrUnauthenticated
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    s.customersMeURI,
		Accept: jsonContentType,
	}

	var customer storeCustomer
	var errors storeErrors
	resp, err := s.retryRequestDecodeJSON(context.TODO(), s.client, reqOptions, user, &customer, &errors)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		if !customer.HasPaymentMethod {
			return ErrNoPaymentMethods
		}
		if !customer.LatestTOSAccepted {
			return ErrTOSNotAccepted
		}
		return nil
	case http.StatusNotFound:
		// Likely because user has no account registered on the pay server
		return fmt.Errorf("cannot get customer details: server says no account exists")
	case http.StatusUnauthorized:
		return ErrInvalidCredentials
	default:
		if len(errors.Errors) == 0 {
			return fmt.Errorf("cannot get customer details: unexpected HTTP code %d", resp.StatusCode)
		}
		return &errors
	}
}
