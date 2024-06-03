// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
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

var requestTimeout = 10 * time.Second

// the LimitTime should be slightly more than 3 times of our http.Client
// Timeout value
var defaultRetryStrategy = retry.LimitCount(6, retry.LimitTime(38*time.Second,
	retry.Exponential{
		Initial: 500 * time.Millisecond,
		Factor:  2.5,
	},
))

var connCheckStrategy = retry.LimitCount(3, retry.LimitTime(38*time.Second,
	retry.Exponential{
		Initial: 900 * time.Millisecond,
		Factor:  1.3,
	},
))

// Config represents the configuration to access the snap store
type Config struct {
	// Store API base URLs. The assertions url is only separate because it can
	// be overridden by its own env var.
	StoreBaseURL      *url.URL
	AssertionsBaseURL *url.URL

	// Authorizer used to authorize requests, can be nil and a default
	// will be used.
	Authorizer Authorizer

	// StoreID is the store id used if we can't get one through the DeviceAndAuthContext.
	StoreID string

	Architecture string
	Series       string

	DetailFields []string
	InfoFields   []string
	// search v2 fields
	FindFields  []string
	DeltaFormat string

	// CacheDownloads is the number of downloads that should be cached
	CacheDownloads int

	// Proxy returns the HTTP proxy to use when talking to the store
	Proxy func(*http.Request) (*url.URL, error)

	// AssertionMaxFormats if set provides a way to override
	// the assertion max formats sent to the store as supported.
	AssertionMaxFormats map[string]int
}

// setBaseURL updates the store API's base URL in the Config. Must not be used
// to change active config.
func (cfg *Config) setBaseURL(u *url.URL) error {
	storeBaseURI, err := storeURL(u)
	if err != nil {
		return err
	}

	assertsBaseURI, err := assertsURL()
	if err != nil {
		return err
	}

	cfg.StoreBaseURL = storeBaseURI
	cfg.AssertionsBaseURL = assertsBaseURI

	return nil
}

// Store represents the ubuntu snap store
type Store struct {
	cfg *Config

	architecture string
	series       string

	noCDN bool

	fallbackStoreID string

	detailFields []string
	infoFields   []string
	findFields   []string
	deltaFormat  string

	auth Authorizer
	// reused http client
	client *http.Client

	dauthCtx DeviceAndAuthContext

	mu                sync.Mutex
	suggestedCurrency string

	cacher downloadCache

	proxy              func(*http.Request) (*url.URL, error)
	proxyConnectHeader http.Header

	userAgent string

	xdeltaCheckLock sync.Mutex
	// whether we should use deltas or not
	shouldUseDeltas *bool
	// which xdelta3 we picked when we checked the deltas
	xdelta3CmdFunc func(args ...string) *exec.Cmd
}

var ErrTooManyRequests = errors.New("too many requests")

// UnexpectedHTTPStatusError represents an error where the store
// returned an unexpected HTTP status code, i.e. a status code that
// doesn't represent success nor an expected error condition with
// known handling (e.g. a 404 when instead presence is always
// expected).
type UnexpectedHTTPStatusError struct {
	OpSummary  string
	StatusCode int
	Method     string
	URL        *url.URL
	OopsID     string
}

func (e *UnexpectedHTTPStatusError) Error() string {
	tpl := "cannot %s: got unexpected HTTP status code %d via %s to %q"
	if e.OopsID != "" {
		tpl += " [%s]"
		return fmt.Sprintf(tpl, e.OpSummary, e.StatusCode, e.Method, e.URL, e.OopsID)
	}
	return fmt.Sprintf(tpl, e.OpSummary, e.StatusCode, e.Method, e.URL)
}

func respToError(resp *http.Response, opSummary string) error {
	if resp.StatusCode == 429 {
		return ErrTooManyRequests
	}
	return &UnexpectedHTTPStatusError{
		OpSummary:  opSummary,
		StatusCode: resp.StatusCode,
		Method:     resp.Request.Method,
		URL:        resp.Request.URL,
		OopsID:     resp.Header.Get("X-Oops-Id"),
	}
}

// endpointURL clones a base URL and updates it with optional path and query.
func endpointURL(base *url.URL, path string, query url.Values) *url.URL {
	u := *base
	if path != "" {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(path, "/")
		u.RawQuery = ""
	}
	if len(query) != 0 {
		u.RawQuery = query.Encode()
	}
	return &u
}

// apiURL returns the system default base API URL.
func apiURL() *url.URL {
	s := "https://api.snapcraft.io/"
	if snapdenv.UseStagingStore() {
		s = "https://api.staging.snapcraft.io/"
	}
	u, _ := url.Parse(s)
	return u
}

// storeURL returns the base store URL, derived from either the given API URL
// or an env var override.
func storeURL(api *url.URL) (*url.URL, error) {
	var override string
	var overrideName string
	// XXX: time to drop FORCE_CPI support
	// XXX: Deprecated but present for backward-compatibility: this used
	// to be "Click Package Index".  Remove this once people have got
	// used to SNAPPY_FORCE_API_URL instead.
	if s := os.Getenv("SNAPPY_FORCE_CPI_URL"); s != "" && strings.HasSuffix(s, "api/v1/") {
		overrideName = "SNAPPY_FORCE_CPI_URL"
		override = strings.TrimSuffix(s, "api/v1/")
	} else if s := os.Getenv("SNAPPY_FORCE_API_URL"); s != "" {
		overrideName = "SNAPPY_FORCE_API_URL"
		override = s
	}
	if override != "" {
		u, err := url.Parse(override)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %s", overrideName, err)
		}
		return u, nil
	}
	return api, nil
}

func assertsURL() (*url.URL, error) {
	if s := os.Getenv("SNAPPY_FORCE_SAS_URL"); s != "" {
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid SNAPPY_FORCE_SAS_URL: %s", err)
		}
		return u, nil
	}

	// nil means fallback to store base url
	return nil, nil
}

func authLocation() string {
	if snapdenv.UseStagingStore() {
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

var defaultStoreDeveloperURL = "https://dashboard.snapcraft.io/"

func storeDeveloperURL() string {
	if snapdenv.UseStagingStore() {
		return "https://dashboard.staging.snapcraft.io/"
	}
	return defaultStoreDeveloperURL
}

var defaultConfig = Config{}

// DefaultConfig returns a copy of the default configuration ready to be adapted.
func DefaultConfig() *Config {
	cfg := defaultConfig
	return &cfg
}

func init() {
	storeBaseURI, err := storeURL(apiURL())
	if err != nil {
		panic(err)
	}
	if storeBaseURI.RawQuery != "" {
		panic("store API URL may not contain query string")
	}
	err = defaultConfig.setBaseURL(storeBaseURI)
	if err != nil {
		panic(err)
	}
	defaultConfig.DetailFields = jsonutil.StructFields((*snapDetails)(nil), "snap_yaml_raw")
	defaultConfig.InfoFields = jsonutil.StructFields((*storeSnap)(nil), "snap-yaml")
	defaultConfig.FindFields = append(jsonutil.StructFields((*storeSnap)(nil),
		"architectures", "created-at", "epoch", "name", "snap-id", "snap-yaml", "resources"),
		"channel")
}

type searchV2Results struct {
	Results   []*storeSearchResult `json:"results"`
	ErrorList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error-list"`
}

type searchResults struct {
	Payload struct {
		Packages []*snapDetails `json:"clickindex:package"`
	} `json:"_embedded"`
}

type sectionResults struct {
	Payload struct {
		Sections []struct{ Name string } `json:"clickindex:sections"`
	} `json:"_embedded"`
}

type CategoryDetails struct {
	Name string `json:"name"`
}

type categoryResults struct {
	Categories []CategoryDetails `json:"categories"`
}

// The default delta format if not configured.
var defaultSupportedDeltaFormat = "xdelta3"

// New creates a new Store with the given access configuration and for given the store id.
func New(cfg *Config, dauthCtx DeviceAndAuthContext) *Store {
	if cfg == nil {
		cfg = &defaultConfig
	}

	detailFields := cfg.DetailFields
	if detailFields == nil {
		detailFields = defaultConfig.DetailFields
	}

	infoFields := cfg.InfoFields
	if infoFields == nil {
		infoFields = defaultConfig.InfoFields
	}

	findFields := cfg.FindFields
	if findFields == nil {
		findFields = defaultConfig.FindFields
	}

	architecture := cfg.Architecture
	if cfg.Architecture == "" {
		architecture = arch.DpkgArchitecture()
	}

	series := cfg.Series
	if cfg.Series == "" {
		series = release.Series
	}

	deltaFormat := cfg.DeltaFormat
	if deltaFormat == "" {
		deltaFormat = defaultSupportedDeltaFormat
	}

	userAgent := snapdenv.UserAgent()
	proxyConnectHeader := http.Header{"User-Agent": []string{userAgent}}

	store := &Store{
		cfg:                cfg,
		series:             series,
		architecture:       architecture,
		noCDN:              osutil.GetenvBool("SNAPPY_STORE_NO_CDN"),
		fallbackStoreID:    cfg.StoreID,
		detailFields:       detailFields,
		infoFields:         infoFields,
		findFields:         findFields,
		dauthCtx:           dauthCtx,
		deltaFormat:        deltaFormat,
		proxy:              cfg.Proxy,
		proxyConnectHeader: proxyConnectHeader,
		userAgent:          userAgent,
	}
	store.client = store.newHTTPClient(&httputil.ClientOptions{
		Timeout:    requestTimeout,
		MayLogBody: true,
	})
	auth := cfg.Authorizer
	if auth == nil {
		if dauthCtx != nil {
			auth = &deviceAuthorizer{endpointURL: store.endpointURL}
		} else {
			auth = UserAuthorizer{}
		}
	}
	store.auth = auth

	store.SetCacheDownloads(cfg.CacheDownloads)

	return store
}

// SetAssertionMaxFormats allows to change the assertion max formats to send
// for a store already in use.
func (s *Store) SetAssertionMaxFormats(maxFormats map[string]int) {
	s.cfg.AssertionMaxFormats = maxFormats
}

// API endpoint paths
const (
	// see https://dashboard.snapcraft.io/docs/
	// XXX: Repeating "api/" here is cumbersome, but the next generation
	// of store APIs will probably drop that prefix (since it now
	// duplicates the hostname), and we may want to switch to v2 APIs
	// one at a time; so it's better to consider that as part of
	// individual endpoint paths.
	searchEndpPath      = "api/v1/snaps/search"
	ordersEndpPath      = "api/v1/snaps/purchases/orders"
	buyEndpPath         = "api/v1/snaps/purchases/buy"
	customersMeEndpPath = "api/v1/snaps/purchases/customers/me"
	sectionsEndpPath    = "api/v1/snaps/sections"
	commandsEndpPath    = "api/v1/snaps/names"
	// v2
	snapActionEndpPath = "v2/snaps/refresh"
	snapInfoEndpPath   = "v2/snaps/info"
	cohortsEndpPath    = "v2/cohorts"
	findEndpPath       = "v2/snaps/find"
	categoriesEndpPath = "v2/snaps/categories"

	deviceNonceEndpPath   = "api/v1/snaps/auth/nonces"
	deviceSessionEndpPath = "api/v1/snaps/auth/sessions"

	assertionsPath = "v2/assertions"
)

var httputilNewHTTPClient = httputil.NewHTTPClient

func (s *Store) newHTTPClient(opts *httputil.ClientOptions) *http.Client {
	if opts == nil {
		opts = &httputil.ClientOptions{}
	}
	opts.Proxy = s.cfg.Proxy
	opts.ProxyConnectHeader = s.proxyConnectHeader
	opts.ExtraSSLCerts = &httputil.ExtraSSLCertsFromDir{
		Dir: dirs.SnapdStoreSSLCertsDir,
	}
	return httputilNewHTTPClient(opts)
}

func (s *Store) defaultSnapQuery() url.Values {
	q := url.Values{}
	if len(s.detailFields) != 0 {
		q.Set("fields", strings.Join(s.detailFields, ","))
	}
	return q
}

func (s *Store) baseURL(defaultURL *url.URL) *url.URL {
	u := defaultURL
	if s.dauthCtx != nil {
		var err error
		_, u, err = s.dauthCtx.ProxyStoreParams(defaultURL)
		if err != nil {
			logger.Debugf("cannot get proxy store parameters from state: %v", err)
		}
	}
	if u != nil {
		return u
	}
	return defaultURL
}

func (s *Store) endpointURL(p string, query url.Values) (*url.URL, error) {
	if err := s.checkStoreOnline(); err != nil {
		return nil, err
	}

	return endpointURL(s.baseURL(s.cfg.StoreBaseURL), p, query), nil
}

// LoginUser logs user in the store and returns the authentication macaroons.
func (s *Store) LoginUser(username, password, otp string) (string, string, error) {
	// most other store network operations use s.endpointURL, which returns an
	// error if the store is offline. this doesn't, so we need to explicitly
	// check.
	if err := s.checkStoreOnline(); err != nil {
		return "", "", err
	}

	macaroon, err := requestStoreMacaroon(s.client)
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

	discharge, err := dischargeAuthCaveat(s.client, loginCaveat, username, password, otp)
	if err != nil {
		return "", "", err
	}

	return macaroon, discharge, nil
}

// EnsureDeviceSession makes sure the store has a device session available.
// Expects the store to have an AuthContext.
func (s *Store) EnsureDeviceSession() error {
	if a, ok := s.auth.(*deviceAuthorizer); ok {
		return a.EnsureDeviceSession(s.dauthCtx, s.client)
	}
	return nil
}

func (s *Store) setStoreID(r *http.Request, apiLevel apiLevel) (customStore bool) {
	storeID := s.fallbackStoreID
	if s.dauthCtx != nil {
		cand, err := s.dauthCtx.StoreID(storeID)
		if err != nil {
			logger.Debugf("cannot get store ID from state: %v", err)
		} else {
			storeID = cand
		}
	}
	if storeID != "" {
		r.Header.Set(hdrSnapDeviceStore[apiLevel], storeID)
		return true
	}
	return false
}

type apiLevel int

const (
	apiV1Endps apiLevel = 0 // api/v1 endpoints
	apiV2Endps apiLevel = 1 // v2 endpoints
)

var (
	hdrSnapDeviceAuthorization = []string{"X-Device-Authorization", "Snap-Device-Authorization"}
	hdrSnapDeviceStore         = []string{"X-Ubuntu-Store", "Snap-Device-Store"}
	hdrSnapDeviceSeries        = []string{"X-Ubuntu-Series", "Snap-Device-Series"}
	hdrSnapDeviceArchitecture  = []string{"X-Ubuntu-Architecture", "Snap-Device-Architecture"}
	hdrSnapClassic             = []string{"X-Ubuntu-Classic", "Snap-Classic"}
)

type deviceAuthNeed int

const (
	deviceAuthPreferred deviceAuthNeed = iota
	deviceAuthCustomStoreOnly
)

// requestOptions specifies parameters for store requests.
type requestOptions struct {
	Method       string
	URL          *url.URL
	Accept       string
	ContentType  string
	APILevel     apiLevel
	ExtraHeaders map[string]string
	Data         []byte

	// DeviceAuthNeed indicates the level of need to supply device
	// authorization for this request, can be:
	//  - deviceAuthPreferred: should be provided if available
	//  - deviceAuthCustomStoreOnly: should be provided only in case
	//    of a custom store
	DeviceAuthNeed deviceAuthNeed
}

func (r *requestOptions) addHeader(k, v string) {
	if r.ExtraHeaders == nil {
		r.ExtraHeaders = make(map[string]string)
	}
	r.ExtraHeaders[k] = v
}

func cancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

var expectedCatalogPreamble = []interface{}{
	json.Delim('{'),
	"_embedded",
	json.Delim('{'),
	"clickindex:package",
	json.Delim('['),
}

type alias struct {
	Name string `json:"name"`
}

type catalogItem struct {
	Name    string   `json:"package_name"`
	Version string   `json:"version"`
	Summary string   `json:"summary"`
	Aliases []alias  `json:"aliases"`
	Apps    []string `json:"apps"`
}

type SnapAdder interface {
	AddSnap(snapName, version, summary string, commands []string) error
}

func decodeCatalog(resp *http.Response, names io.Writer, db SnapAdder) error {
	const what = "decode new commands catalog"
	if resp.StatusCode != 200 {
		return respToError(resp, what)
	}
	dec := json.NewDecoder(resp.Body)
	for _, expectedToken := range expectedCatalogPreamble {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		if token != expectedToken {
			return fmt.Errorf(what+": bad catalog preamble: expected %#v, got %#v", expectedToken, token)
		}
	}

	for dec.More() {
		var v catalogItem
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf(what+": %v", err)
		}
		if v.Name == "" {
			continue
		}
		fmt.Fprintln(names, v.Name)
		if len(v.Apps) == 0 {
			continue
		}

		commands := make([]string, 0, len(v.Aliases)+len(v.Apps))

		for _, alias := range v.Aliases {
			commands = append(commands, alias.Name)
		}
		for _, app := range v.Apps {
			commands = append(commands, snap.JoinSnapApp(v.Name, app))
		}

		if err := db.AddSnap(v.Name, v.Version, v.Summary, commands); err != nil {
			return err
		}
	}

	return nil
}

func decodeJSONBody(resp *http.Response, success interface{}, failure interface{}) error {
	ok := (resp.StatusCode == 200 || resp.StatusCode == 201)
	// always decode on success; decode failures only if body is not empty
	if !ok && resp.ContentLength == 0 {
		return nil
	}
	result := success
	if !ok {
		result = failure
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// retryRequestDecodeJSON calls retryRequest and decodes the response into either success or failure.
func (s *Store) retryRequestDecodeJSON(ctx context.Context, reqOptions *requestOptions, user *auth.UserState, success interface{}, failure interface{}) (resp *http.Response, err error) {
	return httputil.RetryRequest(reqOptions.URL.String(), func() (*http.Response, error) {
		return s.doRequest(ctx, s.client, reqOptions, user)
	}, func(resp *http.Response) error {
		return decodeJSONBody(resp, success, failure)
	}, defaultRetryStrategy)
}

// doRequest does an authenticated request to the store handling a potential macaroon refresh required if needed
func (s *Store) doRequest(ctx context.Context, client *http.Client, reqOptions *requestOptions, user *auth.UserState) (*http.Response, error) {
	authRefreshes := 0
	for {
		req, err := s.newRequest(ctx, reqOptions, user)
		if err != nil {
			return nil, err
		}
		if ctx != nil {
			req = req.WithContext(ctx)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 401 && authRefreshes < 4 {
			// 4 tries: 2 tries for each in case both user
			// and device need refreshing
			wwwAuth := resp.Header.Get("WWW-Authenticate")
			var refreshNeed AuthRefreshNeed
			if strings.Contains(wwwAuth, "needs_refresh=1") {
				// refresh user
				refreshNeed.User = true
			}
			if strings.Contains(wwwAuth, "refresh_device_session=1") {
				// refresh device session
				refreshNeed.Device = true
			}
			if refreshNeed.needed() {
				if a, ok := s.auth.(RefreshingAuthorizer); ok {
					err := a.RefreshAuth(refreshNeed, s.dauthCtx, user, s.client)
					if err != nil {
						return nil, err
					}
					// close previous response and retry
					resp.Body.Close()
					authRefreshes++
					continue
				}
			}
		}

		return resp, err
	}
}

func (s *Store) buildLocationString() (string, error) {
	if s.dauthCtx == nil {
		return "", nil
	}

	cloudInfo, err := s.dauthCtx.CloudInfo()
	if err != nil {
		return "", err
	}

	if cloudInfo == nil {
		return "", nil
	}

	cdnParams := []string{fmt.Sprintf("cloud-name=%q", cloudInfo.Name)}
	if cloudInfo.Region != "" {
		cdnParams = append(cdnParams, fmt.Sprintf("region=%q", cloudInfo.Region))
	}
	if cloudInfo.AvailabilityZone != "" {
		cdnParams = append(cdnParams, fmt.Sprintf("availability-zone=%q", cloudInfo.AvailabilityZone))
	}

	return strings.Join(cdnParams, " "), nil
}

// build a new http.Request with headers for the store
func (s *Store) newRequest(ctx context.Context, reqOptions *requestOptions, user *auth.UserState) (*http.Request, error) {
	var body io.Reader
	if reqOptions.Data != nil {
		body = bytes.NewBuffer(reqOptions.Data)
	}

	req, err := http.NewRequest(reqOptions.Method, reqOptions.URL.String(), body)
	if err != nil {
		return nil, err
	}

	customStore := s.setStoreID(req, reqOptions.APILevel)
	authOpts := AuthorizeOptions{apiLevel: reqOptions.APILevel}
	authOpts.deviceAuth = customStore || reqOptions.DeviceAuthNeed != deviceAuthCustomStoreOnly
	if authOpts.deviceAuth {
		err := s.EnsureDeviceSession()
		if err != nil && err != ErrNoSerial {
			return nil, err
		}
		if err == ErrNoSerial {
			// missing serial assertion, log and continue without device authentication
			logger.Debugf("cannot set device session: %v", err)
		}
	}
	if err := s.auth.Authorize(req, s.dauthCtx, user, &authOpts); err != nil {
		logger.Debugf("cannot authorize store request: %v", err)
	}

	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", reqOptions.Accept)
	req.Header.Set(hdrSnapDeviceArchitecture[reqOptions.APILevel], s.architecture)
	req.Header.Set(hdrSnapDeviceSeries[reqOptions.APILevel], s.series)
	req.Header.Set(hdrSnapClassic[reqOptions.APILevel], strconv.FormatBool(release.OnClassic))
	req.Header.Set("Snap-Device-Capabilities", "default-tracks")
	locationHeader, err := s.buildLocationString()
	if err != nil {
		return nil, err
	}
	if locationHeader != "" {
		req.Header.Set("Snap-Device-Location", locationHeader)
	}
	if cua := ClientUserAgent(ctx); cua != "" {
		req.Header.Set("Snap-Client-User-Agent", cua)
	}
	if reqOptions.APILevel == apiV1Endps {
		req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)
	}

	if reqOptions.ContentType != "" {
		req.Header.Set("Content-Type", reqOptions.ContentType)
	}

	for header, value := range reqOptions.ExtraHeaders {
		req.Header.Set(header, value)
	}

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
//	{
//	  "orders": [
//	    {
//	      "snap_id": "abcd1234efgh5678ijkl9012",
//	      "currency": "USD",
//	      "amount": "2.99",
//	      "state": "Complete",
//	      "refundable_until": null,
//	      "purchase_date": "2016-09-20T15:00:00+00:00"
//	    },
//	    {
//	      "snap_id": "abcd1234efgh5678ijkl9012",
//	      "currency": null,
//	      "amount": null,
//	      "state": "Complete",
//	      "refundable_until": null,
//	      "purchase_date": "2016-09-20T15:00:00+00:00"
//	    }
//	  ]
//	}
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
func (s *Store) decorateOrders(snaps []*snap.Info, user *auth.UserState) error {
	// Mark every non-free snap as must buy until we know better.
	hasPriced := false
	for _, info := range snaps {
		if info.Paid {
			info.MustBuy = true
			hasPriced = true
		}
	}

	if !s.auth.CanAuthorizeForUser(user) {
		return nil
	}

	if !hasPriced {
		return nil
	}

	storeEndpoint, err := s.endpointURL(ordersEndpPath, nil)
	if err != nil {
		return err
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    storeEndpoint,
		Accept: jsonContentType,
	}
	var result ordersResult
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &result, nil)
	if err != nil {
		return err
	}

	if resp.StatusCode == 401 {
		// TODO handle token expiry and refresh
		return ErrInvalidCredentials
	}
	if resp.StatusCode != 200 {
		return respToError(resp, "obtain known orders from store")
	}

	// Make a map of the IDs of bought snaps
	bought := make(map[string]bool)
	for _, order := range result.Orders {
		bought[order.SnapID] = true
	}

	for _, info := range snaps {
		info.MustBuy = mustBuy(info.Paid, bought[info.SnapID])
	}

	return nil
}

// mustBuy determines if a snap requires a payment, based on if it is non-free and if the user has already bought it
func mustBuy(paid bool, bought bool) bool {
	if !paid {
		// If the snap is free, then it doesn't need buying
		return false
	}

	return !bought
}

// A SnapSpec describes a single snap wanted from SnapInfo
type SnapSpec struct {
	Name string
}

// SnapInfo returns the snap.Info for the store-hosted snap matching the given spec, or an error.
func (s *Store) SnapInfo(ctx context.Context, snapSpec SnapSpec, user *auth.UserState) (*snap.Info, error) {
	fields := strings.Join(s.infoFields, ",")

	si, resp, err := s.snapInfo(ctx, snapSpec.Name, fields, user)
	if err != nil {
		return nil, err
	}

	info, err := infoFromStoreInfo(si)
	if err != nil {
		return nil, err
	}

	err = s.decorateOrders([]*snap.Info{info}, user)
	if err != nil {
		logger.Noticef("cannot get user orders: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return info, nil
}

func (s *Store) snapInfo(ctx context.Context, snapName string, fields string, user *auth.UserState) (*storeInfo, *http.Response, error) {
	query := url.Values{}
	query.Set("fields", fields)
	query.Set("architecture", s.architecture)

	u, err := s.endpointURL(path.Join(snapInfoEndpPath, snapName), query)
	if err != nil {
		return nil, nil, err
	}

	reqOptions := &requestOptions{
		Method:   "GET",
		URL:      u,
		APILevel: apiV2Endps,
	}

	var remote storeInfo
	resp, err := s.retryRequestDecodeJSON(ctx, reqOptions, user, &remote, nil)
	if err != nil {
		return nil, nil, err
	}

	// check statusCode
	switch resp.StatusCode {
	case 200:
		// OK
	case 404:
		return nil, nil, ErrSnapNotFound
	default:
		msg := fmt.Sprintf("get details for snap %q", snapName)
		return nil, nil, respToError(resp, msg)
	}

	return &remote, resp, err
}

// SnapInfo checks whether the store-hosted snap matching the given spec exists and returns a reference with it name and snap-id and default channel, or an error.
func (s *Store) SnapExists(ctx context.Context, snapSpec SnapSpec, user *auth.UserState) (naming.SnapRef, *channel.Channel, error) {
	// request the minimal amount information
	fields := "channel-map"

	si, _, err := s.snapInfo(ctx, snapSpec.Name, fields, user)
	if err != nil {
		return nil, nil, err
	}

	return minimalFromStoreInfo(si)
}

// A Search is what you do in order to Find something
type Search struct {
	// Query is a term to search by or a prefix (if Prefix is true)
	Query  string
	Prefix bool

	CommonID string

	// category is "section" in search v1
	Category string
	Private  bool
	Scope    string
}

// Find finds  (installable) snaps from the store, matching the
// given Search.
func (s *Store) Find(ctx context.Context, search *Search, user *auth.UserState) ([]*snap.Info, error) {
	if search.Private && !s.auth.CanAuthorizeForUser(user) {
		return nil, ErrUnauthenticated
	}

	searchTerm := strings.TrimSpace(search.Query)

	// these characters might have special meaning on the search
	// server, and don't form part of a reasonable search, so
	// abort if they're included.
	//
	// "-" might also be special on the server, but it's also a
	// valid part of a package name, so we let it pass
	if strings.ContainsAny(searchTerm, `+=&|><!(){}[]^"~*?:\/`) {
		return nil, ErrBadQuery
	}

	q := url.Values{}
	q.Set("fields", strings.Join(s.findFields, ","))
	q.Set("architecture", s.architecture)

	if search.Private {
		q.Set("private", "true")
	}

	if search.Prefix {
		q.Set("name", searchTerm)
	} else {
		if search.CommonID != "" {
			q.Set("common-id", search.CommonID)
		}
		if searchTerm != "" {
			q.Set("q", searchTerm)
		}
	}

	if search.Category != "" {
		q.Set("category", search.Category)
	}

	// with search v2 all risks are searched by default (same as scope=wide
	// with v1) so we need to restrict channel if scope is not passed.
	if search.Scope == "" {
		q.Set("channel", "stable")
	} else if search.Scope != "wide" {
		return nil, ErrInvalidScope
	}

	if release.OnClassic {
		q.Set("confinement", "strict,classic")
	} else {
		q.Set("confinement", "strict")
	}

	u, err := s.endpointURL(findEndpPath, q)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:   "GET",
		URL:      u,
		Accept:   jsonContentType,
		APILevel: apiV2Endps,
	}

	var searchData searchV2Results

	// TODO: use retryRequestDecodeJSON (may require content-type check there,
	// requires checking other handlers, their tests and store).
	doRequest := func() (*http.Response, error) {
		return s.doRequest(ctx, s.client, reqOptions, user)
	}
	readResponse := func(resp *http.Response) error {
		ok := (resp.StatusCode == 200 || resp.StatusCode == 201)
		ct := resp.Header.Get("Content-Type")
		// always decode on success; decode failures only if body is not empty
		if !ok && (resp.ContentLength == 0 || ct != jsonContentType) {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&searchData)
	}
	resp, err := httputil.RetryRequest(u.String(), doRequest, readResponse, defaultRetryStrategy)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		// fallback to search v1; v2 may not be available on some proxies
		if resp.StatusCode == 404 {
			verstr := resp.Header.Get("Snap-Store-Version")
			ver, err := strconv.Atoi(verstr)
			if err != nil {
				logger.Debugf("Bogus Snap-Store-Version header %q.", verstr)
			} else if ver < 20 {
				return s.findV1(ctx, search, user)
			}
		}
		if len(searchData.ErrorList) > 0 {
			if len(searchData.ErrorList) > 1 {
				logger.Noticef("unexpected number of errors (%d) when trying to search via %q", len(searchData.ErrorList), resp.Request.URL)
			}
			return nil, translateSnapActionError("", "", searchData.ErrorList[0].Code, searchData.ErrorList[0].Message, nil)
		}
		return nil, respToError(resp, "search")
	}

	if ct := resp.Header.Get("Content-Type"); ct != jsonContentType {
		return nil, fmt.Errorf("received an unexpected content type (%q) when trying to search via %q", ct, resp.Request.URL)
	}

	snaps := make([]*snap.Info, len(searchData.Results))
	for i, res := range searchData.Results {
		info, err := infoFromStoreSearchResult(res)
		if err != nil {
			return nil, err
		}
		snaps[i] = info
	}

	err = s.decorateOrders(snaps, user)
	if err != nil {
		logger.Noticef("cannot get user orders: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return snaps, nil
}

func (s *Store) findV1(ctx context.Context, search *Search, user *auth.UserState) ([]*snap.Info, error) {
	// search.Query is already verified for illegal characters by Find()
	searchTerm := strings.TrimSpace(search.Query)
	q := s.defaultSnapQuery()

	if search.Private {
		q.Set("private", "true")
	}

	if search.Prefix {
		q.Set("name", searchTerm)
	} else {
		if search.CommonID != "" {
			q.Set("common_id", search.CommonID)
		}
		if searchTerm != "" {
			q.Set("q", searchTerm)
		}
	}

	// category was "section" in search v1
	if search.Category != "" {
		q.Set("section", search.Category)
	}
	if search.Scope != "" {
		q.Set("scope", search.Scope)
	}

	if release.OnClassic {
		q.Set("confinement", "strict,classic")
	} else {
		q.Set("confinement", "strict")
	}

	u, err := s.endpointURL(searchEndpPath, q)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: halJsonContentType,
	}

	var searchData searchResults
	resp, err := s.retryRequestDecodeJSON(ctx, reqOptions, user, &searchData, nil)
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

	err = s.decorateOrders(snaps, user)
	if err != nil {
		logger.Noticef("cannot get user orders: %v", err)
	}

	s.extractSuggestedCurrency(resp)

	return snaps, nil
}

// Sections retrieves the list of available store sections.
func (s *Store) Sections(ctx context.Context, user *auth.UserState) ([]string, error) {
	u, err := s.endpointURL(sectionsEndpPath, nil)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:         "GET",
		URL:            u,
		Accept:         halJsonContentType,
		DeviceAuthNeed: deviceAuthCustomStoreOnly,
	}

	var sectionData sectionResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &sectionData, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "retrieve sections")
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

// Categories retrieves the list of available store categories.
func (s *Store) Categories(ctx context.Context, user *auth.UserState) ([]CategoryDetails, error) {
	u, err := s.endpointURL(categoriesEndpPath, nil)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:   "GET",
		URL:      u,
		Accept:   jsonContentType,
		APILevel: apiV2Endps,
	}

	var categoryData categoryResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &categoryData, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "retrieve categories")
	}

	if ct := resp.Header.Get("Content-Type"); ct != jsonContentType {
		return nil, fmt.Errorf("received an unexpected content type (%q) when trying to retrieve the categories via %q", ct, resp.Request.URL)
	}

	return categoryData.Categories, nil
}

// WriteCatalogs queries the "commands" endpoint and writes the
// command names into the given io.Writer.
func (s *Store) WriteCatalogs(ctx context.Context, names io.Writer, adder SnapAdder) error {
	u, err := s.endpointURL(commandsEndpPath, nil)
	if err != nil {
		return err
	}

	q := u.Query()
	if release.OnClassic {
		q.Set("confinement", "strict,classic")
	} else {
		q.Set("confinement", "strict")
	}

	u.RawQuery = q.Encode()
	reqOptions := &requestOptions{
		Method:         "GET",
		URL:            u,
		Accept:         halJsonContentType,
		DeviceAuthNeed: deviceAuthCustomStoreOnly,
	}

	// do not log body for catalog updates (its huge)
	client := s.newHTTPClient(&httputil.ClientOptions{
		MayLogBody: false,
		Timeout:    10 * time.Second,
	})
	doRequest := func() (*http.Response, error) {
		return s.doRequest(ctx, client, reqOptions, nil)
	}
	readResponse := func(resp *http.Response) error {
		return decodeCatalog(resp, names, adder)
	}

	resp, err := httputil.RetryRequest(u.String(), doRequest, readResponse, defaultRetryStrategy)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return respToError(resp, "refresh commands catalog")
	}

	return nil
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

func (s *storeErrors) Code() string {
	if len(s.Errors) == 0 {
		return ""
	}
	return s.Errors[0].Code
}

func (s *storeErrors) Error() string {
	if len(s.Errors) == 0 {
		return "internal error: empty store error used as an actual error"
	}
	return s.Errors[0].Error()
}

func buyOptionError(message string) (*client.BuyResult, error) {
	return nil, fmt.Errorf("cannot buy snap: %s", message)
}

// Buy sends a buy request for the specified snap.
// Returns the state of the order: Complete, Cancelled.
func (s *Store) Buy(options *client.BuyOptions, user *auth.UserState) (*client.BuyResult, error) {
	if options.SnapID == "" {
		return buyOptionError("snap ID missing")
	}
	if options.Price <= 0 {
		return buyOptionError("invalid expected price")
	}
	if options.Currency == "" {
		return buyOptionError("currency missing")
	}
	if !s.auth.CanAuthorizeForUser(user) {
		return nil, ErrUnauthenticated
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

	u, err := s.endpointURL(buyEndpPath, nil)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         u,
		Accept:      jsonContentType,
		ContentType: jsonContentType,
		Data:        jsonData,
	}

	var orderDetails order
	var errorInfo storeErrors
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &orderDetails, &errorInfo)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case 200, 201:
		// user already ordered or order successful
		if orderDetails.State == "Cancelled" {
			return buyOptionError("payment cancelled")
		}

		return &client.BuyResult{
			State: orderDetails.State,
		}, nil
	case 400:
		// Invalid price was specified, etc.
		return buyOptionError(fmt.Sprintf("bad request: %v", errorInfo.Error()))
	case 403:
		// Customer account not set up for purchases.
		switch errorInfo.Code() {
		case "no-payment-methods":
			return nil, ErrNoPaymentMethods
		case "tos-not-accepted":
			return nil, ErrTOSNotAccepted
		}
		return buyOptionError(fmt.Sprintf("permission denied: %v", errorInfo.Error()))
	case 404:
		// Likely because customer account or snap ID doesn't exist.
		return buyOptionError(fmt.Sprintf("server says not found: %v", errorInfo.Error()))
	case 402: // Payment Required
		// Payment failed for some reason.
		return nil, ErrPaymentDeclined
	case 401:
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
	if !s.auth.CanAuthorizeForUser(user) {
		return ErrUnauthenticated
	}

	u, err := s.endpointURL(customersMeEndpPath, nil)
	if err != nil {
		return err
	}

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: jsonContentType,
	}

	var customer storeCustomer
	var errors storeErrors
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &customer, &errors)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case 200:
		if !customer.HasPaymentMethod {
			return ErrNoPaymentMethods
		}
		if !customer.LatestTOSAccepted {
			return ErrTOSNotAccepted
		}
		return nil
	case 404:
		// Likely because user has no account registered on the pay server
		return fmt.Errorf("cannot get customer details: server says no account exists")
	case 401:
		return ErrInvalidCredentials
	default:
		if len(errors.Errors) == 0 {
			return fmt.Errorf("cannot get customer details: unexpected HTTP code %d", resp.StatusCode)
		}
		return &errors
	}
}

// abbreviated info structs just for the download info
type storeInfoChannelAbbrev struct {
	Download storeDownload `json:"download"`
}

type storeInfoAbbrev struct {
	// discard anything beyond the first entry
	ChannelMap [1]storeInfoChannelAbbrev `json:"channel-map"`
}

var errUnexpectedConnCheckResponse = errors.New("unexpected response during connection check")

func (s *Store) snapConnCheck() ([]string, error) {
	var hosts []string
	// NOTE: "core" is possibly the only snap that's sure to be in all stores
	//       when we drop "core" in the move to snapd/core18/etc, change this
	infoURL, err := s.endpointURL(path.Join(snapInfoEndpPath, "core"), url.Values{
		// we only want the download URL
		"fields": {"download"},
		// we only need *one* (but can't filter by channel ... yet)
		"architecture": {s.architecture},
	})
	if err != nil {
		return nil, err
	}

	hosts = append(hosts, infoURL.Host)

	var result storeInfoAbbrev
	resp, err := httputil.RetryRequest(infoURL.String(), func() (*http.Response, error) {
		return s.doRequest(context.TODO(), s.client, &requestOptions{
			Method:   "GET",
			URL:      infoURL,
			APILevel: apiV2Endps,
		}, nil)
	}, func(resp *http.Response) error {
		return decodeJSONBody(resp, &result, nil)
	}, connCheckStrategy)

	if err != nil {
		return hosts, err
	}
	resp.Body.Close()

	dlURLraw := result.ChannelMap[0].Download.URL
	dlURL, err := url.ParseRequestURI(dlURLraw)
	if err != nil {
		return hosts, err
	}
	hosts = append(hosts, dlURL.Host)

	cdnHeader, err := s.cdnHeader()
	if err != nil {
		return hosts, err
	}

	reqOptions := downloadReqOpts(dlURL, cdnHeader, nil)
	reqOptions.Method = "HEAD" // not actually a download

	// TODO: We need the HEAD here so that we get redirected to the
	//       right CDN machine. Consider just doing a "net.Dial"
	//       after the redirect here. Suggested in
	// https://github.com/snapcore/snapd/pull/5176#discussion_r193437230
	resp, err = httputil.RetryRequest(dlURLraw, func() (*http.Response, error) {
		return s.doRequest(context.TODO(), s.client, reqOptions, nil)
	}, func(resp *http.Response) error {
		// account for redirect
		hosts[len(hosts)-1] = resp.Request.URL.Host
		return nil
	}, connCheckStrategy)
	if err != nil {
		return hosts, err
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return hosts, errUnexpectedConnCheckResponse
	}

	return hosts, nil
}

var ErrStoreOffline = errors.New("store is marked offline, use 'snap unset system store.access' to go online")

func (s *Store) checkStoreOnline() error {
	if s.dauthCtx == nil {
		return nil
	}

	offline, err := s.dauthCtx.StoreOffline()
	if err != nil {
		return fmt.Errorf("cannot get store access from state: %w", err)
	}

	if offline {
		return ErrStoreOffline
	}

	return nil
}

func (s *Store) ConnectivityCheck() (status map[string]bool, err error) {
	status = make(map[string]bool)

	checkers := []func() ([]string, error){
		s.snapConnCheck,
	}

	for _, checker := range checkers {
		hosts, err := checker()

		// do not swallow errors if the hosts list is empty
		if len(hosts) == 0 && err != nil {
			return nil, err
		}

		for _, host := range hosts {
			status[host] = (err == nil)
		}
	}

	return status, nil
}

func (s *Store) CreateCohorts(ctx context.Context, snaps []string) (map[string]string, error) {
	jsonData, err := json.Marshal(map[string][]string{"snaps": snaps})
	if err != nil {
		return nil, err
	}

	u, err := s.endpointURL(cohortsEndpPath, nil)
	if err != nil {
		return nil, err
	}

	reqOptions := &requestOptions{
		Method:   "POST",
		URL:      u,
		APILevel: apiV2Endps,
		Data:     jsonData,
	}

	var remote struct {
		CohortKeys map[string]string `json:"cohort-keys"`
	}
	resp, err := s.retryRequestDecodeJSON(ctx, reqOptions, nil, &remote, nil)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case 200:
		// OK
	case 404:
		return nil, ErrSnapNotFound
	default:
		return nil, respToError(resp, fmt.Sprintf("create cohorts for %s", strutil.Quoted(snaps)))
	}

	return remote.CohortKeys, nil
}
