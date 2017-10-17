// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

// the LimitTime should be slightly more than 3 times of our http.Client
// Timeout value
var defaultRetryStrategy = retry.LimitCount(5, retry.LimitTime(33*time.Second,
	retry.Exponential{
		Initial: 100 * time.Millisecond,
		Factor:  2.5,
	},
))

func infoFromRemote(d *snapDetails) *snap.Info {
	info := &snap.Info{}
	info.Architectures = d.Architectures
	info.Type = d.Type
	info.Version = d.Version
	info.Epoch = d.Epoch
	info.RealName = d.Name
	info.SnapID = d.SnapID
	info.Revision = snap.R(d.Revision)
	info.EditedTitle = d.Title
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
	info.License = d.License

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

	// fill in the tracks data
	if len(d.ChannelMapList) > 0 {
		info.Channels = make(map[string]*snap.ChannelSnapInfo)
		info.Tracks = make([]string, len(d.ChannelMapList))
		for i, cm := range d.ChannelMapList {
			info.Tracks[i] = cm.Track
			for _, ch := range cm.SnapDetails {
				// nothing in this channel
				if ch.Info == "" {
					continue
				}
				var k string
				if strings.HasPrefix(ch.Channel, cm.Track) {
					k = ch.Channel
				} else {
					k = fmt.Sprintf("%s/%s", cm.Track, ch.Channel)
				}
				info.Channels[k] = &snap.ChannelSnapInfo{
					Revision:    snap.R(ch.Revision),
					Confinement: snap.ConfinementType(ch.Confinement),
					Version:     ch.Version,
					Channel:     ch.Channel,
					Epoch:       ch.Epoch,
					Size:        ch.DownloadSize,
				}
			}
		}
	}

	return info
}

// Config represents the configuration to access the snap store
type Config struct {
	// Store API base URLs. The assertions url is only separate because it can
	// be overridden by its own env var.
	StoreBaseURL      *url.URL
	AssertionsBaseURL *url.URL

	// StoreID is the store id used if we can't get one through the AuthContext.
	StoreID string

	Architecture string
	Series       string

	DetailFields []string
	DeltaFormat  string
}

// SetBaseURL updates the store API's base URL in the Config. Must not be used
// to change active config.
func (cfg *Config) SetBaseURL(u *url.URL) error {
	storeBaseURI, err := storeURL(u)
	if err != nil {
		return err
	}
	assertsBaseURI, err := assertsURL(storeBaseURI)
	if err != nil {
		return err
	}

	cfg.StoreBaseURL = storeBaseURI
	cfg.AssertionsBaseURL = assertsBaseURI

	return nil
}

// Store represents the ubuntu snap store
type Store struct {
	searchURI      *url.URL
	detailsURI     *url.URL
	bulkURI        *url.URL
	assertionsURI  *url.URL
	ordersURI      *url.URL
	buyURI         *url.URL
	customersMeURI *url.URL
	sectionsURI    *url.URL
	commandsURI    *url.URL

	// Device auth endpoints.
	// - deviceNonceURI points to endpoint to get a nonce
	// - deviceSessionURI points to endpoint to get a device session
	deviceNonceURI   *url.URL
	deviceSessionURI *url.URL

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
	// only xdelta3 is supported for now, so check the binary exists here
	// TODO: have a per-format checker instead
	if _, err := getXdelta3Cmd(); err != nil {
		return false
	}

	deltasDefault := release.OnClassic
	return osutil.GetenvBool("SNAPD_USE_DELTAS_EXPERIMENTAL", deltasDefault)
}

func useStaging() bool {
	return osutil.GetenvBool("SNAPPY_USE_STAGING_STORE")
}

// Clone a base URL and update with optional path and query.
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
	if useStaging() {
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

func assertsURL(storeBaseURI *url.URL) (*url.URL, error) {
	if s := os.Getenv("SNAPPY_FORCE_SAS_URL"); s != "" {
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid SNAPPY_FORCE_SAS_URL: %s", err)
		}
		return u, nil
	}
	// XXX: This will eventually become endpointURL(storeBaseURI, "v2/", nil)
	// once new bulk-friendly APIs are designed and implemented.
	return endpointURL(storeBaseURI, "api/v1/snaps", nil), nil
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
	storeBaseURI, err := storeURL(apiURL())
	if err != nil {
		panic(err)
	}
	if storeBaseURI.RawQuery != "" {
		panic("store API URL may not contain query string")
	}
	err = defaultConfig.SetBaseURL(storeBaseURI)
	if err != nil {
		panic(err)
	}
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

	store := &Store{
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

	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	// XXX: These are all required in real system but optional makes it
	// convenient for tests.
	// XXX: Repeating "api/" here is cumbersome, but the next generation
	// of store APIs will probably drop that prefix (since it now
	// duplicates the hostname), and we may want to switch to v2 APIs
	// one at a time; so it's better to consider that as part of
	// individual endpoint paths.
	if cfg.StoreBaseURL != nil {
		store.searchURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/search", nil)
		store.detailsURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/details", nil)
		store.bulkURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/metadata", nil)
		store.ordersURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/purchases/orders", nil)
		store.buyURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/purchases/buy", nil)
		store.customersMeURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/purchases/customers/me", nil)
		store.sectionsURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/sections", nil)
		store.commandsURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/names", nil)
		store.deviceNonceURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/auth/nonces", nil)
		store.deviceSessionURI = endpointURL(cfg.StoreBaseURL, "api/v1/snaps/auth/sessions", nil)
	}
	if cfg.AssertionsBaseURL != nil {
		store.assertionsURI = endpointURL(cfg.AssertionsBaseURL, "assertions", nil)
	}

	return store
}

func (s *Store) defaultSnapQuery() url.Values {
	q := url.Values{}
	if len(s.detailFields) != 0 {
		q.Set("fields", strings.Join(s.detailFields, ","))
	}
	return q
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

// authAvailable returns true if there is a user and/or device session setup
func (s *Store) authAvailable(user *auth.UserState) (bool, error) {
	if hasStoreAuth(user) {
		return true, nil
	} else {
		var device *auth.DeviceState
		var err error
		if s.authContext != nil {
			device, err = s.authContext.Device()
			if err != nil {
				return false, err
			}
		}
		return device != nil && device.SessionMacaroon != "", nil
	}
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
	if s.authContext == nil {
		return fmt.Errorf("user credentials need to be refreshed but update in place only supported in snapd")
	}
	newDischarges, err := refreshDischarges(user)
	if err != nil {
		return err
	}

	curUser, err := s.authContext.UpdateUserAuth(user, newDischarges)
	if err != nil {
		return err
	}
	// update in place
	*user = *curUser

	return nil
}

// refreshDeviceSession will set or refresh the device session in the state
func (s *Store) refreshDeviceSession(device *auth.DeviceState) error {
	if s.authContext == nil {
		return fmt.Errorf("internal error: no authContext")
	}

	nonce, err := requestStoreDeviceNonce(s.deviceNonceURI.String())
	if err != nil {
		return err
	}

	devSessReqParams, err := s.authContext.DeviceSessionRequestParams(nonce)
	if err != nil {
		return err
	}

	session, err := requestDeviceSession(s.deviceSessionURI.String(), devSessReqParams, device.SessionMacaroon)
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

var expectedCatalogPreamble = []interface{}{
	json.Delim('{'),
	"_embedded",
	json.Delim('{'),
	"clickindex:package",
	json.Delim('['),
}

type catalogItem struct {
	Name string `json:"package_name"`
}

func decodeCatalog(resp *http.Response, names io.Writer) error {
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
		if v.Name != "" {
			fmt.Fprintln(names, v.Name)
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
func (s *Store) decorateOrders(snaps []*snap.Info, user *auth.UserState) error {
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

// A SnapSpec describes a single snap wanted from SnapInfo
type SnapSpec struct {
	Name    string
	Channel string
	// AnyChannel can be set to query for any revision independent of channel
	AnyChannel bool
	// Revision can be set to query for an exact revision
	Revision snap.Revision
}

// SnapInfo returns the snap.Info for the store-hosted snap matching the given spec, or an error.
func (s *Store) SnapInfo(snapSpec SnapSpec, user *auth.UserState) (*snap.Info, error) {
	query := s.defaultSnapQuery()

	channel := snapSpec.Channel
	var sel string
	if channel == "" {
		channel = "stable" // default
	}
	if snapSpec.AnyChannel {
		channel = ""
	}
	if !snapSpec.Revision.Unset() {
		query.Set("revision", snapSpec.Revision.String())
		channel = ""
		sel = fmt.Sprintf(" at revision %s", snapSpec.Revision)
	}
	if channel != "" {
		sel = fmt.Sprintf(" in channel %q", channel)
	}
	query.Set("channel", channel)

	u := endpointURL(s.detailsURI, snapSpec.Name, query)
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: halJsonContentType,
	}

	var remote *snapDetails
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &remote, nil)
	if err != nil {
		return nil, err
	}

	// check statusCode
	switch resp.StatusCode {
	case 200:
		// OK
	case 404:
		return nil, ErrSnapNotFound
	default:
		msg := fmt.Sprintf("get details for snap %q%s", snapSpec.Name, sel)
		return nil, respToError(resp, msg)
	}

	info := infoFromRemote(remote)

	err = s.decorateOrders([]*snap.Info{info}, user)
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

	q := s.defaultSnapQuery()

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

	u := endpointURL(s.searchURI, "", q)
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: halJsonContentType,
	}

	var searchData searchResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &searchData, nil)
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
func (s *Store) Sections(user *auth.UserState) ([]string, error) {
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    s.sectionsURI,
		Accept: halJsonContentType,
	}

	var sectionData sectionResults
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &sectionData, nil)
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

// WriteCatalogs queries the "commands" endpoint and writes the
// command names into the given io.Writer.
func (s *Store) WriteCatalogs(names io.Writer) error {
	u := *s.commandsURI

	q := u.Query()
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

	doRequest := func() (*http.Response, error) {
		return s.doRequest(context.TODO(), s.client, reqOptions, nil)
	}
	readResponse := func(resp *http.Response) error {
		return decodeCatalog(resp, names)
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

// RefreshCandidate contains information for the store about the currently
// installed snap so that the store can decide what update we should see
type RefreshCandidate struct {
	SnapID   string
	Revision snap.Revision
	Epoch    snap.Epoch
	Block    []snap.Revision

	// the desired channel
	Channel string
}

// the exact bits that we need to send to the store
type currentSnapJSON struct {
	SnapID      string     `json:"snap_id"`
	Channel     string     `json:"channel"`
	Revision    int        `json:"revision,omitempty"`
	Epoch       snap.Epoch `json:"epoch"`
	Confinement string     `json:"confinement"`
}

type metadataWrapper struct {
	Snaps  []*currentSnapJSON `json:"snaps"`
	Fields []string           `json:"fields"`
}

func currentSnap(cs *RefreshCandidate) *currentSnapJSON {
	// the store gets confused if we send snaps without a snapid
	// (like local ones)
	if cs.SnapID == "" {
		if cs.Revision.Store() {
			logger.Noticef("store.currentSnap got given a RefreshCandidate with an empty SnapID but a store revision!")
		}
		return nil
	}
	if !cs.Revision.Store() {
		logger.Noticef("store.currentSnap got given a RefreshCandidate with a non-empty SnapID but a non-store revision!")
		return nil
	}

	channel := cs.Channel
	if channel == "" {
		channel = "stable"
	}

	return &currentSnapJSON{
		SnapID:   cs.SnapID,
		Channel:  channel,
		Epoch:    cs.Epoch,
		Revision: cs.Revision.N,
		// confinement purposely left empty
	}
}

// query the store for the information about currently offered revisions of snaps
func (s *Store) refreshForCandidates(currentSnaps []*currentSnapJSON, user *auth.UserState) ([]*snapDetails, error) {
	if len(currentSnaps) == 0 {
		// nothing to do
		return nil, nil
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
	resp, err := s.retryRequestDecodeJSON(context.TODO(), reqOptions, user, &updateData, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "query the store for updates")
	}

	s.extractSuggestedCurrency(resp)

	return updateData.Payload.Packages, nil
}

var refreshForCandidates = (*Store).refreshForCandidates

// LookupRefresh returns a snap's store-offered revision information given its refresh candidate.
func (s *Store) LookupRefresh(installed *RefreshCandidate, user *auth.UserState) (*snap.Info, error) {
	cur := currentSnap(installed)
	if cur == nil {
		return nil, ErrLocalSnap
	}

	latest, err := refreshForCandidates(s, []*currentSnapJSON{cur}, user)
	if err != nil {
		return nil, err
	}

	if len(latest) != 1 {
		return nil, ErrSnapNotFound
	}

	rsnap := latest[0]
	if !acceptableUpdate(rsnap, installed) {
		return nil, ErrNoUpdateAvailable
	}

	return infoFromRemote(rsnap), nil
}

// ListRefresh returns the available updates for a list of refresh candidates.
// NOTE ListRefresh can return nil, nil if e.g. all local snaps are passed in
func (s *Store) ListRefresh(installed []*RefreshCandidate, user *auth.UserState) (snaps []*snap.Info, err error) {

	candidateMap := map[string]*RefreshCandidate{}
	currentSnaps := make([]*currentSnapJSON, 0, len(installed))
	for _, cs := range installed {
		cur := currentSnap(cs)
		if cur == nil {
			continue
		}
		currentSnaps = append(currentSnaps, cur)
		candidateMap[cs.SnapID] = cs
	}

	latest, err := s.refreshForCandidates(currentSnaps, user)
	if err != nil {
		return nil, err
	}

	toRefresh := make([]*snap.Info, 0, len(latest))
	for _, rsnap := range latest {
		if !acceptableUpdate(rsnap, candidateMap[rsnap.SnapID]) {
			continue
		}

		toRefresh = append(toRefresh, infoFromRemote(rsnap))
	}

	return toRefresh, nil
}

func acceptableUpdate(remote *snapDetails, installed *RefreshCandidate) bool {
	rrev := snap.R(remote.Revision)
	return !(rrev == installed.Revision || findRev(rrev, installed.Block))
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
	return fmt.Sprintf("sha3-384 mismatch for %q: got %s but expected %s", e.name, e.sha3_384, e.targetSha3_384)
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

		if len(downloadInfo.Deltas) == 1 {
			err := s.downloadAndApplyDelta(name, targetPath, downloadInfo, pbar, user)
			if err == nil {
				return nil
			}
			// We revert to normal downloads if there is any error.
			logger.Noticef("Cannot download or apply deltas for %s: %v", name, err)
		}
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

	authAvail, err := s.authAvailable(user)
	if err != nil {
		return err
	}

	url := downloadInfo.AnonDownloadURL
	if url == "" || authAvail {
		url = downloadInfo.DownloadURL
	}

	if downloadInfo.Size == 0 || resume < downloadInfo.Size {
		err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, resume, pbar)
	} else {
		// we're done! check the hash though
		h := crypto.SHA3_384.New()
		if _, err := w.Seek(0, os.SEEK_SET); err != nil {
			return err
		}
		if _, err := io.Copy(h, w); err != nil {
			return err
		}
		actualSha3 := fmt.Sprintf("%x", h.Sum(nil))
		if downloadInfo.Sha3_384 != actualSha3 {
			err = HashError{name, actualSha3, downloadInfo.Sha3_384}
		}
	}
	// If hashsum is incorrect retry once
	if _, ok := err.(HashError); ok {
		logger.Debugf("Hashsum error on download: %v", err.Error())
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
		httputil.MaybeLogRetryAttempt(reqOptions.URL.String(), attempt, startTime)

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
			if httputil.ShouldRetryError(attempt, finalErr) {
				continue
			}
			break
		}

		if httputil.ShouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		}

		defer resp.Body.Close()

		switch resp.StatusCode {
		case 200, 206: // OK, Partial Content
		case 402: // Payment Required

			return fmt.Errorf("please buy %s before installing it.", name)
		default:
			return &DownloadError{Code: resp.StatusCode, URL: resp.Request.URL}
		}

		if pbar == nil {
			pbar = progress.Null
		}
		pbar.Start(name, float64(resp.ContentLength))
		mw := io.MultiWriter(w, h, pbar)
		_, finalErr = io.Copy(mw, resp.Body)
		pbar.Finished()
		if finalErr != nil {
			if httputil.ShouldRetryError(attempt, finalErr) {
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

	authAvail, err := s.authAvailable(user)
	if err != nil {
		return err
	}

	url := deltaInfo.AnonDownloadURL
	if url == "" || authAvail {
		url = deltaInfo.DownloadURL
	}

	return download(context.TODO(), deltaName, deltaInfo.Sha3_384, url, user, s, w, 0, pbar)
}

func getXdelta3Cmd(args ...string) (*exec.Cmd, error) {
	switch {
	case osutil.ExecutableExists("xdelta3"):
		return exec.Command("xdelta3", args...), nil
	case osutil.FileExists(filepath.Join(dirs.SnapMountDir, "/core/current/usr/bin/xdelta3")):
		return osutil.CommandFromCore("/usr/bin/xdelta3", args...)
	}
	return nil, fmt.Errorf("cannot find xdelta3 binary in PATH or core snap")
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
	cmd, err := getXdelta3Cmd(xdelta3Args...)
	if err != nil {
		return err
	}

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
	v := url.Values{}
	v.Set("max-format", strconv.Itoa(assertType.MaxSupportedFormat()))
	u := endpointURL(s.assertionsURI, path.Join(assertType.Name, path.Join(primaryKey...)), v)

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: asserts.MediaType,
	}

	var asrt asserts.Assertion

	resp, err := httputil.RetryRequest(reqOptions.URL.String(), func() (*http.Response, error) {
		return s.doRequest(context.TODO(), s.client, reqOptions, user)
	}, func(resp *http.Response) error {
		var e error
		if resp.StatusCode == 200 {
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
					// best-effort
					headers, _ := asserts.HeadersFromPrimaryKey(assertType, primaryKey)
					return &asserts.NotFoundError{
						Type:    assertType,
						Headers: headers,
					}
				}
				return fmt.Errorf("assertion service error: [%s] %q", svcErr.Title, svcErr.Detail)
			}
		}
		return e
	}, defaultRetryStrategy)

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
		URL:         s.buyURI,
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

		return &BuyResult{
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
