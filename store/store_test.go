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

package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

func TestStore(t *testing.T) { TestingT(t) }

type configTestSuite struct{}

var _ = Suite(&configTestSuite{})

var (
	// this is what snap.E("0") looks like when decoded into an interface{} (the /^i/ is for "interface")
	iZeroEpoch = map[string]interface{}{
		"read":  []interface{}{0.},
		"write": []interface{}{0.},
	}
	// ...and this is snap.E("5*")
	iFiveStarEpoch = map[string]interface{}{
		"read":  []interface{}{4., 5.},
		"write": []interface{}{5.},
	}
)

func (suite *configTestSuite) TestSetBaseURL(c *C) {
	// Validity check to prove at least one URI changes.
	cfg := store.DefaultConfig()
	c.Assert(cfg.StoreBaseURL.String(), Equals, "https://api.snapcraft.io/")

	u, err := url.Parse("http://example.com/path/prefix/")
	c.Assert(err, IsNil)
	err = cfg.SetBaseURL(u)
	c.Assert(err, IsNil)

	c.Check(cfg.StoreBaseURL.String(), Equals, "http://example.com/path/prefix/")
	c.Check(cfg.AssertionsBaseURL, IsNil)
}

func (suite *configTestSuite) TestSetBaseURLStoreOverrides(c *C) {
	cfg := store.DefaultConfig()
	c.Assert(cfg.SetBaseURL(store.ApiURL()), IsNil)
	c.Check(cfg.StoreBaseURL, Matches, store.ApiURL().String()+".*")

	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "https://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")
	cfg = store.DefaultConfig()
	c.Assert(cfg.SetBaseURL(store.ApiURL()), IsNil)
	c.Check(cfg.StoreBaseURL.String(), Equals, "https://force-api.local/")
	c.Check(cfg.AssertionsBaseURL, IsNil)
}

func (suite *configTestSuite) TestSetBaseURLStoreURLBadEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://example.com"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")

	cfg := store.DefaultConfig()
	err := cfg.SetBaseURL(store.ApiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse \"?://example.com\"?: missing protocol scheme")
}

func (suite *configTestSuite) TestSetBaseURLAssertsOverrides(c *C) {
	cfg := store.DefaultConfig()
	c.Assert(cfg.SetBaseURL(store.ApiURL()), IsNil)
	c.Check(cfg.AssertionsBaseURL, IsNil)

	c.Assert(os.Setenv("SNAPPY_FORCE_SAS_URL", "https://force-sas.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_SAS_URL", "")
	cfg = store.DefaultConfig()
	c.Assert(cfg.SetBaseURL(store.ApiURL()), IsNil)
	c.Check(cfg.AssertionsBaseURL, Matches, "https://force-sas.local/.*")
}

func (suite *configTestSuite) TestSetBaseURLAssertsURLBadEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_SAS_URL", "://example.com"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_SAS_URL", "")

	cfg := store.DefaultConfig()
	err := cfg.SetBaseURL(store.ApiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_SAS_URL: parse \"?://example.com\"?: missing protocol scheme")
}

const (
	// Store API paths/patterns.
	authNoncesPath     = "/api/v1/snaps/auth/nonces"
	authSessionPath    = "/api/v1/snaps/auth/sessions"
	buyPath            = "/api/v1/snaps/purchases/buy"
	customersMePath    = "/api/v1/snaps/purchases/customers/me"
	detailsPathPattern = "/api/v1/snaps/details/.*"
	ordersPath         = "/api/v1/snaps/purchases/orders"
	searchPath         = "/api/v1/snaps/search"
	sectionsPath       = "/api/v1/snaps/sections"
	// v2
	findPath        = "/v2/snaps/find"
	snapActionPath  = "/v2/snaps/refresh"
	infoPathPattern = "/v2/snaps/info/.*"
	cohortsPath     = "/v2/cohorts"
	categoriesPath  = "/v2/snaps/categories"
)

// Build details path for a snap name.
func detailsPath(snapName string) string {
	return strings.Replace(detailsPathPattern, ".*", snapName, 1)
}

// Build info path for a snap name.
func infoPath(snapName string) string {
	return strings.Replace(infoPathPattern, ".*", snapName, 1)
}

// Assert that a request is roughly as expected. Useful in fakes that should
// only attempt to handle a specific request.
func assertRequest(c *C, r *http.Request, method, pathPattern string) {
	pathMatch, err := regexp.MatchString("^"+pathPattern+"$", r.URL.Path)
	c.Assert(err, IsNil)
	if r.Method != method || !pathMatch {
		c.Fatalf("request didn't match (expected %s %s, got %s %s)", method, pathPattern, r.Method, r.URL.Path)
	}
}

type baseStoreSuite struct {
	testutil.BaseTest

	device *auth.DeviceState
	user   *auth.UserState

	ctx context.Context

	logbuf *bytes.Buffer
}

const (
	exModel = `type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: baz-3000
architecture: armhf
gadget: gadget
kernel: kernel
store: my-brand-store-id
timestamp: 2016-08-20T13:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`

	exSerial = `type: serial
authority-id: my-brand
brand-id: my-brand
model: baz-3000
serial: 9999
device-key:
    AcbBTQRWhcGAARAAtJGIguK7FhSyRxL/6jvdy0zAgGCjC1xVNFzeF76p5G8BXNEEHZUHK+z8Gr2J
    inVrpvhJhllf5Ob2dIMH2YQbC9jE1kjbzvuauQGDqk6tNQm0i3KDeHCSPgVN+PFXPwKIiLrh66Po
    AC7OfR1rFUgCqu0jch0H6Nue0ynvEPiY4dPeXq7mCdpDr5QIAM41L+3hg0OdzvO8HMIGZQpdF6jP
    7fkkVMROYvHUOJ8kknpKE7FiaNNpH7jK1qNxOYhLeiioX0LYrdmTvdTWHrSKZc82ZmlDjpKc4hUx
    VtTXMAysw7CzIdREPom/vJklnKLvZt+Wk5AEF5V5YKnuT3pY+fjVMZ56GtTEeO/Er/oLk/n2xUK5
    fD5DAyW/9z0ygzwTbY5IuWXyDfYneL4nXwWOEgg37Z4+8mTH+ftTz2dl1x1KIlIR2xo0kxf9t8K+
    jlr13vwF1+QReMCSUycUsZ2Eep5XhjI+LG7G1bMSGqodZTIOXLkIy6+3iJ8Z/feIHlJ0ELBDyFbl
    Yy04Sf9LI148vJMsYenonkoWejWdMi8iCUTeaZydHJEUBU/RbNFLjCWa6NIUe9bfZgLiOOZkps54
    +/AL078ri/tGjo/5UGvezSmwrEoWJyqrJt2M69N2oVDLJcHeo2bUYPtFC2Kfb2je58JrJ+llifdg
    rAsxbnHXiXyVimUAEQEAAQ==
device-key-sha3-384: EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
timestamp: 2016-08-24T21:55:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`

	exDeviceSessionRequest = `type: device-session-request
brand-id: my-brand
model: baz-3000
serial: 9999
nonce: @NONCE@
timestamp: 2016-08-24T21:55:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`
)

type testDauthContext struct {
	c      *C
	device *auth.DeviceState

	deviceMu         sync.Mutex
	deviceGetWitness func()

	user *auth.UserState

	proxyStoreID  string
	proxyStoreURL *url.URL

	storeID string

	storeOffline bool

	cloudInfo *auth.CloudInfo
}

func (dac *testDauthContext) Device() (*auth.DeviceState, error) {
	dac.deviceMu.Lock()
	defer dac.deviceMu.Unlock()
	freshDevice := auth.DeviceState{}
	if dac.device != nil {
		freshDevice = *dac.device
	}
	if dac.deviceGetWitness != nil {
		dac.deviceGetWitness()
	}
	return &freshDevice, nil
}

func (dac *testDauthContext) UpdateDeviceAuth(d *auth.DeviceState, newSessionMacaroon string) (*auth.DeviceState, error) {
	dac.deviceMu.Lock()
	defer dac.deviceMu.Unlock()
	dac.c.Assert(d, DeepEquals, dac.device)
	updated := *dac.device
	updated.SessionMacaroon = newSessionMacaroon
	*dac.device = updated
	return &updated, nil
}

func (dac *testDauthContext) UpdateUserAuth(u *auth.UserState, newDischarges []string) (*auth.UserState, error) {
	dac.c.Assert(u, DeepEquals, dac.user)
	updated := *dac.user
	updated.StoreDischarges = newDischarges
	return &updated, nil
}

func (dac *testDauthContext) StoreID(fallback string) (string, error) {
	if dac.storeID != "" {
		return dac.storeID, nil
	}
	return fallback, nil
}

func (dac *testDauthContext) DeviceSessionRequestParams(nonce string) (*store.DeviceSessionRequestParams, error) {
	model, err := asserts.Decode([]byte(exModel))
	if err != nil {
		return nil, err
	}

	serial, err := asserts.Decode([]byte(exSerial))
	if err != nil {
		return nil, err
	}

	sessReq, err := asserts.Decode([]byte(strings.Replace(exDeviceSessionRequest, "@NONCE@", nonce, 1)))
	if err != nil {
		return nil, err
	}

	return &store.DeviceSessionRequestParams{
		Request: sessReq.(*asserts.DeviceSessionRequest),
		Serial:  serial.(*asserts.Serial),
		Model:   model.(*asserts.Model),
	}, nil
}

func (dac *testDauthContext) ProxyStoreParams(defaultURL *url.URL) (string, *url.URL, error) {
	if dac.proxyStoreID != "" {
		return dac.proxyStoreID, dac.proxyStoreURL, nil
	}
	return "", defaultURL, nil
}

func (dac *testDauthContext) StoreOffline() (bool, error) {
	return dac.storeOffline, nil
}

func (dac *testDauthContext) CloudInfo() (*auth.CloudInfo, error) {
	return dac.cloudInfo, nil
}

func makeTestMacaroon() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	if err != nil {
		return nil, err
	}
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", store.UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func makeTestDischarge() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("shared-key"), "third-party-caveat", store.UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func makeTestRefreshDischargeResponse() (string, error) {
	m, err := macaroon.New([]byte("shared-key"), "refreshed-third-party-caveat", store.UbuntuoneLocation)
	if err != nil {
		return "", err
	}

	return auth.MacaroonSerialize(m)
}

func createTestUser(userID int, root, discharge *macaroon.Macaroon) (*auth.UserState, error) {
	serializedMacaroon, err := auth.MacaroonSerialize(root)
	if err != nil {
		return nil, err
	}
	serializedDischarge, err := auth.MacaroonSerialize(discharge)
	if err != nil {
		return nil, err
	}

	return &auth.UserState{
		ID:              userID,
		Username:        "test-user",
		Macaroon:        serializedMacaroon,
		Discharges:      []string{serializedDischarge},
		StoreMacaroon:   serializedMacaroon,
		StoreDischarges: []string{serializedDischarge},
	}, nil
}

func createTestDevice() *auth.DeviceState {
	return &auth.DeviceState{
		Brand:           "some-brand",
		SessionMacaroon: "device-macaroon",
		Serial:          "9999",
	}
}

func (s *baseStoreSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	os.Setenv("SNAPD_DEBUG", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAPD_DEBUG") })

	var restoreLogger func()
	s.logbuf, restoreLogger = logger.MockLogger()
	s.AddCleanup(restoreLogger)

	s.ctx = context.TODO()

	s.device = createTestDevice()

	root, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	discharge, err := makeTestDischarge()
	c.Assert(err, IsNil)
	s.user, err = createTestUser(1, root, discharge)
	c.Assert(err, IsNil)

	store.MockDefaultRetryStrategy(&s.BaseTest, retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	)))
}

type storeTestSuite struct {
	baseStoreSuite
}

var _ = Suite(&storeTestSuite{})

func (s *storeTestSuite) SetUpTest(c *C) {
	s.baseStoreSuite.SetUpTest(c)
}

func expectedAuthorization(c *C, user *auth.UserState) string {
	var buf bytes.Buffer

	root, err := auth.MacaroonDeserialize(user.StoreMacaroon)
	c.Assert(err, IsNil)
	discharge, err := auth.MacaroonDeserialize(user.StoreDischarges[0])
	c.Assert(err, IsNil)
	discharge.Bind(root.Signature())

	serializedMacaroon, err := auth.MacaroonSerialize(root)
	c.Assert(err, IsNil)
	serializedDischarge, err := auth.MacaroonSerialize(discharge)
	c.Assert(err, IsNil)

	fmt.Fprintf(&buf, `Macaroon root="%s", discharge="%s"`, serializedMacaroon, serializedDischarge)
	return buf.String()
}

var (
	userAgent = snapdenv.UserAgent()
)

func (s *storeTestSuite) TestDoRequestSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check user authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, expectedAuthorization(c, s.user))
		// check device authorization is set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, dauthCtx)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (s *storeTestSuite) TestDoRequestDoesNotSetAuthForLocalOnlyUser(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check no user authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, "")
		// check device authorization is set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	localUser := &auth.UserState{
		ID:       11,
		Username: "test-user",
		Macaroon: "snapd-macaroon",
	}

	dauthCtx := &testDauthContext{c: c, device: s.device, user: localUser}
	sto := store.New(&store.Config{}, dauthCtx)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, localUser)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (s *storeTestSuite) TestDoRequestAuthNoSerial(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check user authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, expectedAuthorization(c, s.user))
		// check device authorization was not set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// no serial and no device macaroon => no device auth
	s.device.Serial = ""
	s.device.SessionMacaroon = ""
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, dauthCtx)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (s *storeTestSuite) TestDoRequestRefreshesAuth(c *C) {
	refresh, err := makeTestRefreshDischargeResponse()
	c.Assert(err, IsNil)
	c.Check(s.user.StoreDischarges[0], Not(Equals), refresh)

	// mock refresh response
	refreshDischargeEndpointHit := false
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fmt.Sprintf(`{"discharge_macaroon": "%s"}`, refresh))
		refreshDischargeEndpointHit = true
	}))
	defer mockSSOServer.Close()
	store.UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

	// mock store response (requiring auth refresh)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, expectedAuthorization(c, s.user))
		if s.user.StoreDischarges[0] == refresh {
			io.WriteString(w, "response-data")
		} else {
			w.Header().Set("WWW-Authenticate", "Macaroon needs_refresh=1")
			w.WriteHeader(401)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, dauthCtx)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
	c.Check(refreshDischargeEndpointHit, Equals, true)
}

func (s *storeTestSuite) TestDoRequestForwardsRefreshAuthFailure(c *C) {
	// mock refresh response
	refreshDischargeEndpointHit := false
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreInvalidLoginCode)
		io.WriteString(w, mockStoreInvalidLogin)
		refreshDischargeEndpointHit = true
	}))
	defer mockSSOServer.Close()
	store.UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

	// mock store response (requiring auth refresh)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, expectedAuthorization(c, s.user))
		w.Header().Set("WWW-Authenticate", "Macaroon needs_refresh=1")
		w.WriteHeader(401)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, dauthCtx)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	c.Assert(err, Equals, store.ErrInvalidCredentials)
	c.Check(response, IsNil)
	c.Check(refreshDischargeEndpointHit, Equals, true)
}

func (s *storeTestSuite) TestEnsureDeviceSession(c *C) {
	deviceSessionRequested := 0
	// mock store response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		switch r.URL.Path {
		case authNoncesPath:
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case authSessionPath:
			// validity of request
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			var req map[string]string
			err = json.Unmarshal(jsonReq, &req)
			c.Assert(err, IsNil)
			c.Check(strings.HasPrefix(req["device-session-request"], "type: device-session-request\n"), Equals, true)
			c.Check(strings.HasPrefix(req["serial-assertion"], "type: serial\n"), Equals, true)
			c.Check(strings.HasPrefix(req["model-assertion"], "type: model\n"), Equals, true)
			authorization := r.Header.Get("X-Device-Authorization")
			c.Assert(authorization, Equals, "")
			deviceSessionRequested++
			io.WriteString(w, `{"macaroon": "fresh-session-macaroon"}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)

	// make sure device session is not set
	s.device.SessionMacaroon = ""
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	err := sto.EnsureDeviceSession()
	c.Assert(err, IsNil)

	c.Check(s.device.SessionMacaroon, Equals, "fresh-session-macaroon")
	c.Check(deviceSessionRequested, Equals, 1)
}

func (s *storeTestSuite) TestEnsureDeviceSessionSerialisation(c *C) {
	var deviceSessionRequested int32
	// mock store response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		switch r.URL.Path {
		case authNoncesPath:
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case authSessionPath:
			// validity of request
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			var req map[string]string
			err = json.Unmarshal(jsonReq, &req)
			c.Assert(err, IsNil)
			c.Check(strings.HasPrefix(req["device-session-request"], "type: device-session-request\n"), Equals, true)
			c.Check(strings.HasPrefix(req["serial-assertion"], "type: serial\n"), Equals, true)
			c.Check(strings.HasPrefix(req["model-assertion"], "type: model\n"), Equals, true)
			authorization := r.Header.Get("X-Device-Authorization")
			c.Assert(authorization, Equals, "")
			atomic.AddInt32(&deviceSessionRequested, 1)
			io.WriteString(w, `{"macaroon": "fresh-session-macaroon"}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)

	wgGetDevice := new(sync.WaitGroup)

	// make sure device session is not set
	s.device.SessionMacaroon = ""
	dauthCtx := &testDauthContext{
		c:                c,
		device:           s.device,
		deviceGetWitness: wgGetDevice.Done,
	}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	wg := new(sync.WaitGroup)

	sto.SessionLock()

	// try to acquire 10 times a device session in parallel;
	// block these flows until all goroutines have acquired the original
	// device state which is without a session, then let them run
	for i := 0; i < 10; i++ {
		wgGetDevice.Add(1)
		wg.Add(1)
		go func() {
			err := sto.EnsureDeviceSession()
			c.Assert(err, IsNil)
			wg.Done()
		}()
	}

	wgGetDevice.Wait()
	dauthCtx.deviceGetWitness = nil
	// all flows have got the original device state
	// let them run
	sto.SessionUnlock()
	// wait for the 10 flows to be done
	wg.Wait()

	c.Check(s.device.SessionMacaroon, Equals, "fresh-session-macaroon")
	// we acquired a session from the store only once
	c.Check(int(deviceSessionRequested), Equals, 1)
}

func (s *storeTestSuite) TestDoRequestSetsAndRefreshesDeviceAuth(c *C) {
	deviceSessionRequested := false
	refreshSessionRequested := false
	expiredAuth := `Macaroon root="expired-session-macaroon"`
	// mock store response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		switch r.URL.Path {
		case "/":
			authorization := r.Header.Get("X-Device-Authorization")
			if authorization == "" {
				c.Fatalf("device authentication missing")
			} else if authorization == expiredAuth {
				w.Header().Set("WWW-Authenticate", "Macaroon refresh_device_session=1")
				w.WriteHeader(401)
			} else {
				c.Check(authorization, Equals, `Macaroon root="refreshed-session-macaroon"`)
				io.WriteString(w, "response-data")
			}
		case authNoncesPath:
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case authSessionPath:
			// validity of request
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			var req map[string]string
			err = json.Unmarshal(jsonReq, &req)
			c.Assert(err, IsNil)
			c.Check(strings.HasPrefix(req["device-session-request"], "type: device-session-request\n"), Equals, true)
			c.Check(strings.HasPrefix(req["serial-assertion"], "type: serial\n"), Equals, true)
			c.Check(strings.HasPrefix(req["model-assertion"], "type: model\n"), Equals, true)

			authorization := r.Header.Get("X-Device-Authorization")
			if authorization == "" {
				io.WriteString(w, `{"macaroon": "expired-session-macaroon"}`)
				deviceSessionRequested = true
			} else {
				c.Check(authorization, Equals, expiredAuth)
				io.WriteString(w, `{"macaroon": "refreshed-session-macaroon"}`)
				refreshSessionRequested = true
			}
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)

	// make sure device session is not set
	s.device.SessionMacaroon = ""
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	reqOptions := store.NewRequestOptions("GET", mockServerURL)

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	c.Assert(err, IsNil)
	defer response.Body.Close()

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
	c.Check(deviceSessionRequested, Equals, true)
	c.Check(refreshSessionRequested, Equals, true)
}

func (s *storeTestSuite) TestDoRequestSetsAndRefreshesBothAuths(c *C) {
	refresh, err := makeTestRefreshDischargeResponse()
	c.Assert(err, IsNil)
	c.Check(s.user.StoreDischarges[0], Not(Equals), refresh)

	// mock refresh response
	refreshDischargeEndpointHit := false
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fmt.Sprintf(`{"discharge_macaroon": "%s"}`, refresh))
		refreshDischargeEndpointHit = true
	}))
	defer mockSSOServer.Close()
	store.UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

	refreshSessionRequested := false
	expiredAuth := `Macaroon root="expired-session-macaroon"`
	// mock store response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		switch r.URL.Path {
		case "/":
			authorization := r.Header.Get("Authorization")
			c.Check(authorization, Equals, expectedAuthorization(c, s.user))
			if s.user.StoreDischarges[0] != refresh {
				w.Header().Set("WWW-Authenticate", "Macaroon needs_refresh=1")
				w.WriteHeader(401)
				return
			}

			devAuthorization := r.Header.Get("X-Device-Authorization")
			if devAuthorization == "" {
				c.Fatalf("device authentication missing")
			} else if devAuthorization == expiredAuth {
				w.Header().Set("WWW-Authenticate", "Macaroon refresh_device_session=1")
				w.WriteHeader(401)
			} else {
				c.Check(devAuthorization, Equals, `Macaroon root="refreshed-session-macaroon"`)
				io.WriteString(w, "response-data")
			}
		case authNoncesPath:
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case authSessionPath:
			// validity of request
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			var req map[string]string
			err = json.Unmarshal(jsonReq, &req)
			c.Assert(err, IsNil)
			c.Check(strings.HasPrefix(req["device-session-request"], "type: device-session-request\n"), Equals, true)
			c.Check(strings.HasPrefix(req["serial-assertion"], "type: serial\n"), Equals, true)
			c.Check(strings.HasPrefix(req["model-assertion"], "type: model\n"), Equals, true)

			authorization := r.Header.Get("X-Device-Authorization")
			if authorization == "" {
				c.Fatalf("expecting only refresh")
			} else {
				c.Check(authorization, Equals, expiredAuth)
				io.WriteString(w, `{"macaroon": "refreshed-session-macaroon"}`)
				refreshSessionRequested = true
			}
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)

	// make sure device session is expired
	s.device.SessionMacaroon = "expired-session-macaroon"
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	reqOptions := store.NewRequestOptions("GET", mockServerURL)

	resp, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Check(resp.StatusCode, Equals, 200)

	responseData, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
	c.Check(refreshDischargeEndpointHit, Equals, true)
	c.Check(refreshSessionRequested, Equals, true)
}

func (s *storeTestSuite) TestDoRequestSetsExtraHeaders(c *C) {
	// Custom headers are applied last.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, `customAgent`)
		c.Check(r.Header.Get("X-Foo-Header"), Equals, `Bar`)
		c.Check(r.Header.Get("Content-Type"), Equals, `application/bson`)
		c.Check(r.Header.Get("Accept"), Equals, `application/hal+bson`)
		c.Check(r.Header.Get("Snap-Device-Capabilities"), Equals, "default-tracks")
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	sto := store.New(&store.Config{}, nil)
	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)
	reqOptions.ExtraHeaders = map[string]string{
		"X-Foo-Header": "Bar",
		"Content-Type": "application/bson",
		"Accept":       "application/hal+bson",
		"User-Agent":   "customAgent",
	}

	response, err := sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (s *storeTestSuite) TestLoginUser(c *C) {
	macaroon, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	serializedMacaroon, err := auth.MacaroonSerialize(macaroon)
	c.Assert(err, IsNil)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, fmt.Sprintf(`{"macaroon": "%s"}`, serializedMacaroon))
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	store.MacaroonACLAPI = mockServer.URL + "/acl/"

	discharge, err := makeTestDischarge()
	c.Assert(err, IsNil)
	serializedDischarge, err := auth.MacaroonSerialize(discharge)
	c.Assert(err, IsNil)
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, fmt.Sprintf(`{"discharge_macaroon": "%s"}`, serializedDischarge))
	}))
	c.Assert(mockSSOServer, NotNil)
	defer mockSSOServer.Close()
	store.UbuntuoneDischargeAPI = mockSSOServer.URL + "/tokens/discharge"

	sto := store.New(nil, nil)
	userMacaroon, userDischarge, err := sto.LoginUser("username", "password", "otp")

	c.Assert(err, IsNil)
	c.Check(userMacaroon, Equals, serializedMacaroon)
	c.Check(userDischarge, Equals, serializedDischarge)
}

func (s *storeTestSuite) TestLoginUserDeveloperAPIError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "{}")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	store.MacaroonACLAPI = mockServer.URL + "/acl/"

	sto := store.New(nil, nil)
	userMacaroon, userDischarge, err := sto.LoginUser("username", "password", "otp")

	c.Assert(err, ErrorMatches, "cannot get snap access permission from store: .*")
	c.Check(userMacaroon, Equals, "")
	c.Check(userDischarge, Equals, "")
}

func (s *storeTestSuite) TestLoginUserSSOError(c *C) {
	macaroon, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	serializedMacaroon, err := auth.MacaroonSerialize(macaroon)
	c.Assert(err, IsNil)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, fmt.Sprintf(`{"macaroon": "%s"}`, serializedMacaroon))
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	store.MacaroonACLAPI = mockServer.URL + "/acl/"

	errorResponse := `{"code": "some-error"}`
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, errorResponse)
	}))
	c.Assert(mockSSOServer, NotNil)
	defer mockSSOServer.Close()
	store.UbuntuoneDischargeAPI = mockSSOServer.URL + "/tokens/discharge"

	sto := store.New(nil, nil)
	userMacaroon, userDischarge, err := sto.LoginUser("username", "password", "otp")

	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: .*")
	c.Check(userMacaroon, Equals, "")
	c.Check(userDischarge, Equals, "")
}

const (
	funkyAppSnapID = "1e21e12ex4iim2xj1g2ul6f12f1"

	helloWorldSnapID = "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
	// instance key used in refresh action of snap hello-world_foo, salt "123"
	helloWorldFooInstanceKeyWithSalt = helloWorldSnapID + ":IDKVhLy-HUyfYGFKcsH4V-7FVG7hLGs4M5zsraZU5tk"
	helloWorldDeveloperID            = "canonical"
)

const mockOrdersJSON = `{
  "orders": [
    {
      "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
      "currency": "USD",
      "amount": "1.99",
      "state": "Complete",
      "refundable_until": "2015-07-15 18:46:21",
      "purchase_date": "2016-09-20T15:00:00+00:00"
    },
    {
      "snap_id": "1e21e12ex4iim2xj1g2ul6f12f1",
      "currency": "USD",
      "amount": "1.99",
      "state": "Complete",
      "refundable_until": "2015-07-17 11:33:29",
      "purchase_date": "2016-09-20T15:00:00+00:00"
    }
  ]
}`

const mockOrderResponseJSON = `{
  "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
  "currency": "USD",
  "amount": "1.99",
  "state": "Complete",
  "refundable_until": "2015-07-15 18:46:21",
  "purchase_date": "2016-09-20T15:00:00+00:00"
}`

const mockSingleOrderJSON = `{
  "orders": [
    {
      "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
      "currency": "USD",
      "amount": "1.99",
      "state": "Complete",
      "refundable_until": "2015-07-15 18:46:21",
      "purchase_date": "2016-09-20T15:00:00+00:00"
    }
  ]
}`

// acquired via:
// http --pretty=format --print b https://api.snapcraft.io/v2/snaps/info/hello-world architecture==amd64 fields==architectures,base,confinement,links,contact,created-at,description,download,epoch,license,name,prices,private,publisher,revision,snap-id,snap-yaml,summary,title,type,version,media,common-ids,website Snap-Device-Series:16 | xsel -b
// on 2022-10-20. Then, by hand:
// set prices to {"EUR": "0.99", "USD": "1.23"},
// set base in first channel-map entry to "bogus-base",
// set snap-yaml in first channel-map entry to the one from the 'edge', plus the following pastiche:
// apps:
//
//	  content-plug:
//		   command: bin/content-plug
//		   plugs: [shared-content-plug]
//
// plugs:
//
//	  shared-content-plug:
//		   interface: content
//		   target: import
//		   content: mylib
//		   default-provider: test-snapd-content-slot
//
// slots:
//
//	  shared-content-slot:
//		   interface: content
//		   content: mylib
//		   read:
//		     - /
//
// Then change edge entry to have different revision, version and "released-at" to something randomish
const mockInfoJSON = `{
    "channel-map": [
        {
            "architectures": [
                "all"
            ],
            "base": "bogus-base",
            "channel": {
                "architecture": "amd64",
                "name": "stable",
                "released-at": "2019-04-17T16:47:59.117114+00:00",
                "risk": "stable",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2019-04-17T16:43:58.548661+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "b07bdb78e762c2e6020c75fafc92055b323a6f8da3ab42a3963da5ade386aba11f77e3c8f919b8aa23f3aa5c06c844f9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_29.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 29,
            "snap-yaml": "name: hello-world\nversion: 6.4\narchitectures: [ all ]\nsummary: The 'hello-world' of snaps\ndescription: |\n    This is a simple snap example that includes a few interesting binaries\n    to demonstrate snaps and their confinement.\n    * hello-world.env  - dump the env of commands run inside app sandbox\n    * hello-world.evil - show how snappy sandboxes binaries\n    * hello-world.sh   - enter interactive shell that runs in app sandbox\n    * hello-world      - simply output text\napps:\n env:\n   command: bin/env\n evil:\n   command: bin/evil\n sh:\n   command: bin/sh\n hello-world:\n   command: bin/echo\n content-plug:\n   command: bin/content-plug\n   plugs: [shared-content-plug]\nplugs:\n  shared-content-plug:\n    interface: content\n    target: import\n    content: mylib\n    default-provider: test-snapd-content-slot\nslots:\n  shared-content-slot:\n    interface: content\n    content: mylib\n    read:\n      - /\n",
            "type": "app",
            "version": "6.4"
        },
        {
            "architectures": [
                "all"
            ],
            "base": null,
            "channel": {
                "architecture": "amd64",
                "name": "candidate",
                "released-at": "2019-04-17T16:47:59.117114+00:00",
                "risk": "candidate",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2019-04-17T16:43:58.548661+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "b07bdb78e762c2e6020c75fafc92055b323a6f8da3ab42a3963da5ade386aba11f77e3c8f919b8aa23f3aa5c06c844f9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_29.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 29,
            "type": "app",
            "version": "6.4"
        },
        {
            "architectures": [
                "all"
            ],
            "base": null,
            "channel": {
                "architecture": "amd64",
                "name": "beta",
                "released-at": "2019-04-17T16:48:09.906850+00:00",
                "risk": "beta",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2019-04-17T16:43:58.548661+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "b07bdb78e762c2e6020c75fafc92055b323a6f8da3ab42a3963da5ade386aba11f77e3c8f919b8aa23f3aa5c06c844f9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_29.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 29,
            "type": "app",
            "version": "6.4"
        },
        {
            "architectures": [
                "all"
            ],
            "base": null,
            "channel": {
                "architecture": "amd64",
                "name": "edge",
                "released-at": "2022-10-19T17:00:00+00:00",
                "risk": "edge",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2019-04-17T16:43:58.548661+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "b07bdb78e762c2e6020c75fafc92055b323a6f8da3ab42a3963da5ade386aba11f77e3c8f919b8aa23f3aa5c06c844f9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_30.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 30,
            "snap-yaml": "",
            "type": "app",
            "version": "6.5"
        }
    ],
    "default-track": null,
    "name": "hello-world",
    "snap": {
        "contact": "mailto:snaps@canonical.com",
        "description": "This is a simple hello world example.",
        "license": "MIT",
        "links": {
            "contact": [
                "mailto:snaps@canonical.com"
            ]
        },
        "media": [
            {
                "height": 256,
                "type": "icon",
                "url": "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "width": 256
            },
            {
                "height": 118,
                "type": "screenshot",
                "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png",
                "width": 199
            },
            {
                "height": null,
                "type": "video",
                "url": "https://vimeo.com/194577403",
                "width": null
            }
        ],
        "categories": [
            {
                "featured": true,
                "name": "featured"
            },
            {
                "featured": false,
                "name": "productivity"
            }
        ],
        "name": "hello-world",
        "prices": {"EUR": "0.99", "USD": "1.23"},
        "private": true,
        "publisher": {
            "display-name": "Canonical",
            "id": "canonical",
            "username": "canonical",
            "validation": "verified"
        },
        "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
        "summary": "The 'hello-world' of snaps",
        "title": "Hello World",
        "website": null
    },
    "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
}`

func (s *storeTestSuite) TestInfo(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.UserAgent(), Equals, userAgent)

		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Path, Matches, ".*/hello-world")

		query := r.URL.Query()
		c.Check(query.Get("fields"), Equals, "abc,def")
		c.Check(query.Get("architecture"), Equals, arch.DpkgArchitecture())

		w.Header().Set("X-Suggested-Currency", "GBP")
		w.WriteHeader(200)
		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		InfoFields:   []string{"abc", "def"},
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
	c.Check(result.Architectures, DeepEquals, []string{"all"})
	c.Check(result.Revision, Equals, snap.R(29))
	c.Check(result.SnapID, Equals, helloWorldSnapID)
	c.Check(result.Publisher, Equals, snap.StoreAccount{
		ID:          "canonical",
		Username:    "canonical",
		DisplayName: "Canonical",
		Validation:  "verified",
	})
	c.Check(result.Version, Equals, "6.4")
	c.Check(result.Sha3_384, Matches, `[[:xdigit:]]{96}`)
	c.Check(result.Size, Equals, int64(20480))
	c.Check(result.Channel, Equals, "stable")
	c.Check(result.Description(), Equals, "This is a simple hello world example.")
	c.Check(result.Summary(), Equals, "The 'hello-world' of snaps")
	c.Check(result.Title(), Equals, "Hello World") // TODO: have this updated to be different to the name
	c.Check(result.License, Equals, "MIT")
	c.Check(result.Prices, DeepEquals, map[string]float64{"EUR": 0.99, "USD": 1.23})
	c.Check(result.Paid, Equals, true)
	c.Check(result.Media, DeepEquals, snap.MediaInfos{
		{
			Type:   "icon",
			URL:    "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
			Width:  256,
			Height: 256,
		}, {
			Type:   "screenshot",
			URL:    "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png",
			Width:  199,
			Height: 118,
		}, {
			Type: "video",
			URL:  "https://vimeo.com/194577403",
		},
	})
	c.Check(result.Categories, DeepEquals, []snap.CategoryInfo{
		{
			Featured: true,
			Name:     "featured",
		}, {
			Featured: false,
			Name:     "productivity",
		},
	})
	c.Check(result.MustBuy, Equals, true)
	c.Check(result.Links(), DeepEquals, map[string][]string{
		"contact": {"mailto:snaps@canonical.com"},
	})
	c.Check(result.Contact(), Equals, "mailto:snaps@canonical.com")
	c.Check(result.Base, Equals, "bogus-base")
	c.Check(result.Epoch.String(), Equals, "0")
	c.Check(sto.SuggestedCurrency(), Equals, "GBP")
	c.Check(result.Private, Equals, true)

	c.Check(snap.Validate(result), IsNil)

	// validate the plugs/slots (only here because we faked stuff in the JSON)
	c.Assert(result.Plugs, HasLen, 1)
	plug := result.Plugs["shared-content-plug"]
	c.Check(plug.Name, Equals, "shared-content-plug")
	c.Check(plug.Snap, DeepEquals, result)
	c.Check(plug.Apps, HasLen, 1)
	c.Check(plug.Apps["content-plug"].Command, Equals, "bin/content-plug")

	c.Assert(result.Slots, HasLen, 1)
	slot := result.Slots["shared-content-slot"]
	c.Check(slot.Name, Equals, "shared-content-slot")
	c.Check(slot.Snap, DeepEquals, result)
	c.Check(slot.Apps, HasLen, 5)
	c.Check(slot.Apps["content-plug"].Command, Equals, "bin/content-plug")
}

func (s *storeTestSuite) TestInfoBadResponses(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			// This one should work.
			// (strictly speaking the channel map item should at least have a "channel" member)
			io.WriteString(w, `{"channel-map": [{}], "snap": {"name":"hello"}}`)
		case 2:
			// "not found" (no channel map)
			io.WriteString(w, `{"snap":{"name":"hello"}}`)
		case 3:
			// "not found" (same)
			io.WriteString(w, `{"channel-map": [], "snap": {"name":"hello"}}`)
		case 4:
			// bad price
			io.WriteString(w, `{"channel-map": [{}], "snap": {"name":"hello","prices":{"XPD": "Palladium?!?"}}}`)
		default:
			c.Errorf("expected at most 4 calls, now on #%d", n)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		InfoFields:   []string{},
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	info, err := sto.SnapInfo(s.ctx, store.SnapSpec{Name: "hello"}, nil)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "hello")

	info, err = sto.SnapInfo(s.ctx, store.SnapSpec{Name: "hello"}, nil)
	c.Check(err, Equals, store.ErrSnapNotFound)
	c.Check(info, IsNil)

	info, err = sto.SnapInfo(s.ctx, store.SnapSpec{Name: "hello"}, nil)
	c.Check(err, Equals, store.ErrSnapNotFound)
	c.Check(info, IsNil)

	info, err = sto.SnapInfo(s.ctx, store.SnapSpec{Name: "hello"}, nil)
	c.Check(err, ErrorMatches, `.* invalid syntax`)
	c.Check(info, IsNil)
}

func (s *storeTestSuite) TestInfoDefaultChannelIsStable(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.URL.Path, Matches, ".*/hello-world")

		w.WriteHeader(200)

		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
	c.Check(result.SnapID, Equals, helloWorldSnapID)
	c.Check(result.Channel, Equals, "stable")
}

func (s *storeTestSuite) TestInfo500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	_, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world": got unexpected HTTP status code 500 via GET to "http://.*?/info/hello-world.*"`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestInfo500Once(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		n++
		if n > 1 {
			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(200)
			io.WriteString(w, mockInfoJSON)
		} else {
			w.WriteHeader(500)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestInfoAndChannels(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		switch n {
		case 0:
			c.Check(r.URL.Path, Matches, ".*/hello-world")

			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(200)

			io.WriteString(w, mockInfoJSON)
		default:
			c.Fatalf("unexpected request to %q", r.URL.Path)
		}
		n++
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Check(result.InstanceName(), Equals, "hello-world")
	expected := map[string]*snap.ChannelSnapInfo{
		"latest/stable": {
			Revision:    snap.R(29),
			Version:     "6.4",
			Confinement: snap.StrictConfinement,
			Channel:     "latest/stable",
			Size:        20480,
			Epoch:       snap.E("0"),
			ReleasedAt:  time.Date(2019, 4, 17, 16, 47, 59, 117114000, time.UTC),
		},
		"latest/candidate": {
			Revision:    snap.R(29),
			Version:     "6.4",
			Confinement: snap.StrictConfinement,
			Channel:     "latest/candidate",
			Size:        20480,
			Epoch:       snap.E("0"),
			ReleasedAt:  time.Date(2019, 4, 17, 16, 47, 59, 117114000, time.UTC),
		},
		"latest/beta": {
			Revision:    snap.R(29),
			Version:     "6.4",
			Confinement: snap.StrictConfinement,
			Channel:     "latest/beta",
			Size:        20480,
			Epoch:       snap.E("0"),
			ReleasedAt:  time.Date(2019, 4, 17, 16, 48, 9, 906850000, time.UTC),
		},
		"latest/edge": {
			Revision:    snap.R(30),
			Version:     "6.5",
			Confinement: snap.StrictConfinement,
			Channel:     "latest/edge",
			Size:        20480,
			Epoch:       snap.E("0"),
			ReleasedAt:  time.Date(2022, 10, 19, 17, 0, 0, 0, time.UTC),
		},
	}
	for k, v := range result.Channels {
		c.Check(v, DeepEquals, expected[k], Commentf("%q", k))
	}
	c.Check(result.Channels, HasLen, len(expected))

	c.Check(snap.Validate(result), IsNil)
}

func (s *storeTestSuite) TestInfoMoreChannels(c *C) {
	// NB this tests more channels, but still only one architecture
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		// following is just an aligned version of:
		// http https://api.snapcraft.io/v2/snaps/info/go architecture==amd64 fields==channel Snap-Device-Series:16 | jq -c '.["channel-map"] | .[]'
		io.WriteString(w, `{"channel-map": [
{"channel":{"architecture":"amd64","name":"stable",        "released-at":"2018-12-17T09:17:16.288554+00:00","risk":"stable",   "track":"latest"}},
{"channel":{"architecture":"amd64","name":"edge",          "released-at":"2018-11-06T00:46:03.348730+00:00","risk":"edge",     "track":"latest"}},
{"channel":{"architecture":"amd64","name":"1.11/stable",   "released-at":"2018-12-17T09:17:48.847205+00:00","risk":"stable",   "track":"1.11"}},
{"channel":{"architecture":"amd64","name":"1.11/candidate","released-at":"2018-12-17T00:10:05.864910+00:00","risk":"candidate","track":"1.11"}},
{"channel":{"architecture":"amd64","name":"1.10/stable",   "released-at":"2018-12-17T06:53:57.915517+00:00","risk":"stable",   "track":"1.10"}},
{"channel":{"architecture":"amd64","name":"1.10/candidate","released-at":"2018-12-17T00:04:13.413244+00:00","risk":"candidate","track":"1.10"}},
{"channel":{"architecture":"amd64","name":"1.9/stable",    "released-at":"2018-06-13T02:23:06.338145+00:00","risk":"stable",   "track":"1.9"}},
{"channel":{"architecture":"amd64","name":"1.8/stable",    "released-at":"2018-02-07T23:08:59.152984+00:00","risk":"stable",   "track":"1.8"}},
{"channel":{"architecture":"amd64","name":"1.7/stable",    "released-at":"2017-06-02T01:16:52.640258+00:00","risk":"stable",   "track":"1.7"}},
{"channel":{"architecture":"amd64","name":"1.6/stable",    "released-at":"2017-05-17T21:18:42.224979+00:00","risk":"stable",   "track":"1.6"}}
]}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	result, err := sto.SnapInfo(s.ctx, store.SnapSpec{Name: "eh"}, nil)
	c.Assert(err, IsNil)
	expected := map[string]*snap.ChannelSnapInfo{
		"latest/stable":  {Channel: "latest/stable", ReleasedAt: time.Date(2018, 12, 17, 9, 17, 16, 288554000, time.UTC)},
		"latest/edge":    {Channel: "latest/edge", ReleasedAt: time.Date(2018, 11, 6, 0, 46, 3, 348730000, time.UTC)},
		"1.6/stable":     {Channel: "1.6/stable", ReleasedAt: time.Date(2017, 5, 17, 21, 18, 42, 224979000, time.UTC)},
		"1.7/stable":     {Channel: "1.7/stable", ReleasedAt: time.Date(2017, 6, 2, 1, 16, 52, 640258000, time.UTC)},
		"1.8/stable":     {Channel: "1.8/stable", ReleasedAt: time.Date(2018, 2, 7, 23, 8, 59, 152984000, time.UTC)},
		"1.9/stable":     {Channel: "1.9/stable", ReleasedAt: time.Date(2018, 6, 13, 2, 23, 6, 338145000, time.UTC)},
		"1.10/stable":    {Channel: "1.10/stable", ReleasedAt: time.Date(2018, 12, 17, 6, 53, 57, 915517000, time.UTC)},
		"1.10/candidate": {Channel: "1.10/candidate", ReleasedAt: time.Date(2018, 12, 17, 0, 4, 13, 413244000, time.UTC)},
		"1.11/stable":    {Channel: "1.11/stable", ReleasedAt: time.Date(2018, 12, 17, 9, 17, 48, 847205000, time.UTC)},
		"1.11/candidate": {Channel: "1.11/candidate", ReleasedAt: time.Date(2018, 12, 17, 0, 10, 5, 864910000, time.UTC)},
	}
	for k, v := range result.Channels {
		c.Check(v, DeepEquals, expected[k], Commentf("%q", k))
	}
	c.Check(result.Channels, HasLen, len(expected))
	c.Check(result.Tracks, DeepEquals, []string{"latest", "1.11", "1.10", "1.9", "1.8", "1.7", "1.6"})
}

func (s *storeTestSuite) TestInfoNonDefaults(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.Header.Get("Snap-Device-Store"), Equals, "foo")
		c.Check(r.URL.Path, Matches, ".*/hello-world$")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, "21")
		c.Check(r.URL.Query().Get("architecture"), Equals, "archXYZ")

		w.WriteHeader(200)
		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "foo"
	sto := store.New(cfg, nil)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
}

func (s *storeTestSuite) TestStoreIDFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "my-brand-store-id")

		w.WriteHeader(200)
		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "fallback"
	sto := store.New(cfg, &testDauthContext{c: c, device: s.device, storeID: "my-brand-store-id"})

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
}

func (s *storeTestSuite) TestLocation(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		storeID := r.Header.Get("Snap-Device-Location")
		c.Check(storeID, Equals, `cloud-name="gcp" region="us-west1" availability-zone="us-west1-b"`)

		w.WriteHeader(200)
		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	sto := store.New(cfg, &testDauthContext{c: c, device: s.device, cloudInfo: &auth.CloudInfo{Name: "gcp", Region: "us-west1", AvailabilityZone: "us-west1-b"}})

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
}

func (s *storeTestSuite) TestProxyStoreFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)

		w.WriteHeader(200)
		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	nowhereURL, err := url.Parse("http://nowhere.invalid")
	c.Assert(err, IsNil)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = nowhereURL
	sto := store.New(cfg, &testDauthContext{
		c:             c,
		device:        s.device,
		proxyStoreID:  "foo",
		proxyStoreURL: mockServerURL,
	})

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
}

func (s *storeTestSuite) TestProxyStoreFromAuthContextURLFallback(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)

		w.WriteHeader(200)
		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	sto := store.New(cfg, &testDauthContext{
		c:      c,
		device: s.device,
		// mock an assertion that has id but no url
		proxyStoreID:  "foo",
		proxyStoreURL: nil,
	})

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
}

func (s *storeTestSuite) TestInfoOopses(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.URL.Path, Matches, ".*/hello-world")

		w.Header().Set("X-Oops-Id", "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f")
		w.WriteHeader(500)

		io.WriteString(w, `{"oops": "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	_, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world": got unexpected HTTP status code 5.. via GET to "http://\S+" \[OOPS-[[:xdigit:]]*\]`)
}

const mockExistsJSON = `{
  "channel-map": [
    {
      "channel": {
        "architecture": "amd64",
        "name": "stable",
        "released-at": "2019-04-17T17:40:12.922344+00:00",
        "risk": "stable",
        "track": "latest"
      }
    },
    {
      "channel": {
        "architecture": "amd64",
        "name": "candidate",
        "released-at": "2017-05-17T21:17:00.205237+00:00",
        "risk": "candidate",
        "track": "latest"
      }
    },
    {
      "channel": {
        "architecture": "amd64",
        "name": "beta",
        "released-at": "2017-05-17T21:17:00.205019+00:00",
        "risk": "beta",
        "track": "latest"
      }
    },
    {
      "channel": {
        "architecture": "amd64",
        "name": "edge",
        "released-at": "2017-05-17T21:17:00.205167+00:00",
        "risk": "edge",
        "track": "latest"
      }
    }
  ],
  "default-track": null,
  "name": "hello",
  "snap": {},
  "snap-id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6"
}`

func (s *storeTestSuite) TestExists(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.UserAgent(), Equals, userAgent)

		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Path, Matches, ".*/hello")

		query := r.URL.Query()
		c.Check(query.Get("fields"), Equals, "channel-map")
		c.Check(query.Get("architecture"), Equals, arch.DpkgArchitecture())

		w.WriteHeader(200)
		io.WriteString(w, mockExistsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello",
	}
	ref, ch, err := sto.SnapExists(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Check(ref.SnapName(), Equals, "hello")
	c.Check(ref.ID(), Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
	c.Check(ch, DeepEquals, &channel.Channel{
		Architecture: "amd64",
		Name:         "stable",
		Risk:         "stable",
	})
}

func (s *storeTestSuite) TestExistsNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.URL.Path, Matches, ".*/hello")

		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello",
	}
	ref, ch, err := sto.SnapExists(s.ctx, spec, nil)
	c.Assert(err, Equals, store.ErrSnapNotFound)
	c.Assert(ref, IsNil)
	c.Assert(ch, IsNil)
}

// acquired via:
//
// http --pretty=format --print b https://api.snapcraft.io/v2/snaps/info/no:such:package architecture==amd64 fields==architectures,base,confinement,contact,created-at,description,download,epoch,license,name,prices,private,publisher,revision,snap-id,snap-yaml,summary,title,type,version,media,common-ids Snap-Device-Series:16 | xsel -b
//
// on 2018-06-14
const MockNoDetailsJSON = `{
    "error-list": [
        {
            "code": "resource-not-found",
            "message": "No snap named 'no:such:package' found in series '16'."
        }
    ]
}`

func (s *storeTestSuite) TestNoInfo(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		c.Check(r.URL.Path, Matches, ".*/no-such-pkg")

		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	// the actual test
	spec := store.SnapSpec{
		Name: "no-such-pkg",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, NotNil)
	c.Assert(result, IsNil)
}

/*
	acquired via looking at the query snapd does for "snap find 'hello-world of snaps' --narrow" (on core) and adding size=1:

curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64" 'https://api.snapcraft.io/api/v1/snaps/search?confinement=strict&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Clicense%2Cbase%2Cmedia%2Csupport_url%2Ccontact%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cdeveloper_name%2Cdeveloper_validation%2Cprivate%2Cconfinement%2Ccommon_ids&q=hello-world+of+snaps&size=1' | python -m json.tool | xsel -b

And then add base and prices, increase title's length, and remove the _links dict
*/
const mockSearchJSON = `{
    "_embedded": {
        "clickindex:package": [
            {
                "anon_download_url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
                "architecture": [
                    "all"
                ],
                "base": "bare-base",
                "binary_filesize": 20480,
                "channel": "stable",
                "common_ids": [],
                "confinement": "strict",
                "contact": "mailto:snaps@canonical.com",
                "content": "application",
                "description": "This is a simple hello world example.",
                "developer_id": "canonical",
                "developer_name": "Canonical",
                "developer_validation": "verified",
                "download_sha3_384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
                "download_url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
                "last_updated": "2016-07-12T16:37:23.960632+00:00",
                "license": "MIT",
                "media": [
                    {
                        "type": "icon",
                        "url": "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png"
                    },
                    {
                        "type": "screenshot",
                        "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png"
                    }
                ],
                "categories": [
                    {
                        "featured": true,
                        "name": "featured"
                    },
                    {
                        "featured": false,
                        "name": "productivity"
                    }
                ],
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {"EUR": 2.99, "USD": 3.49},
                "private": false,
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 27,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "The 'hello-world' of snaps",
                "support_url": "",
                "title": "This Is The Most Fantastical Snap of Hello World",
                "version": "6.3"
            }
        ]
    }
}
`

// curl -H 'Snap-Device-Series:16' 'https://api.snapcraft.io/v2/snaps/find?architecture=amd64&confinement=strict%2Cclassic&fields=base%2Cconfinement%2Ccontact%2Cdescription%2Cdownload%2Clicense%2Clinks%2Cprices%2Cprivate%2Cpublisher%2Crevision%2Cstore-url%2Csummary%2Ctitle%2Ctype%2Cversion%2Cmedia%2Cchannel&q=hello-world+of+snaps'
const mockSearchJSONv2 = `
{
	"results" : [
	   {
              "name": "hello-world",
              "revision": {
                "base": "bare-base",
                "channel": "stable",
                "confinement": "strict",
                "download": {
                  "size": 20480
                },
                "revision": 27,
                "common-ids" : ["aaa", "bbb"],
                "type": "app",
                "version": "6.3"
              },
              "snap": {
                "contact": "mailto:snaps@canonical.com",
                "description": "This is a simple hello world example.",
                "license": "MIT",
                "links": {
                  "contact": [
                    "mailto:snaps@canonical.com"
                  ],
                  "website": [
                    "https://ubuntu.com"
                  ]
                },
                "media": [
                  {
                    "type": "icon",
                    "url": "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png"
                  },
                  {
                    "type": "screenshot",
                    "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png"
                  }
                ],
                "categories": [
                  {
                    "featured": true,
                    "name": "featured"
                  },
                  {
                    "featured": false,
                    "name": "productivity"
                  }
                ],
                "prices": {"EUR": "2.99", "USD": "3.49"},
                "private": false,
                "publisher": {
                  "display-name": "Canonical",
                  "id": "canonical",
                  "username": "canonical",
                  "validation": "verified"
                },
                "store-url": "https://snapcraft.io/hello-world",
                "summary": "The 'hello-world' of snaps",
                "website": "https://ubuntu.com",
                "title": "This Is The Most Fantastical Snap of Hello World"
              },
              "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
	   }
	]
 }
`

const storeVerWithV1Search = "18"

func forceSearchV1(w http.ResponseWriter) {
	w.Header().Set("Snap-Store-Version", storeVerWithV1Search)
	http.Error(w, http.StatusText(404), 404)
}

func (s *storeTestSuite) TestFindV1Queries(c *C) {
	n := 0
	var v1Fallback bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, findPath) {
			forceSearchV1(w)
			return
		}
		v1Fallback = true
		assertRequest(c, r, "GET", searchPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")
		section := query.Get("section")

		c.Check(r.URL.Path, Matches, ".*/search")
		c.Check(query.Get("fields"), Equals, "abc,def")

		// write test json so that Find doesn't re-try due to json decoder EOF error
		io.WriteString(w, "{}")

		switch n {
		case 0:
			c.Check(name, Equals, "hello")
			c.Check(q, Equals, "")
			c.Check(query.Get("scope"), Equals, "")
			c.Check(section, Equals, "")
		case 1:
			c.Check(name, Equals, "")
			c.Check(q, Equals, "hello")
			c.Check(query.Get("scope"), Equals, "wide")
			c.Check(section, Equals, "")
		case 2:
			c.Check(name, Equals, "")
			c.Check(q, Equals, "")
			c.Check(query.Get("scope"), Equals, "")
			c.Check(section, Equals, "db")
		case 3:
			c.Check(name, Equals, "")
			c.Check(q, Equals, "hello")
			c.Check(query.Get("scope"), Equals, "")
			c.Check(section, Equals, "db")
		default:
			c.Fatalf("what? %d", n)
		}

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	for _, query := range []store.Search{
		{Query: "hello", Prefix: true},
		{Query: "hello", Scope: "wide"},
		{Category: "db"},
		{Query: "hello", Category: "db"},
	} {
		sto.Find(s.ctx, &query, nil)
	}
	c.Check(n, Equals, 4)
	c.Check(v1Fallback, Equals, true)
}

/*
	acquired via:

curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://api.snapcraft.io/api/v1/snaps/sections'
*/
const MockSectionsJSON = `{
  "_embedded": {
    "clickindex:sections": [
      {
        "name": "featured"
      }, 
      {
        "name": "database"
      }
    ]
  }, 
  "_links": {
    "self": {
      "href": "http://api.snapcraft.io/api/v1/snaps/sections"
    }
  }
}
`

func (s *storeTestSuite) TestSectionsQuery(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", sectionsPath)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		switch n {
		case 0:
			// All good.
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(200)
		io.WriteString(w, MockSectionsJSON)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	sections, err := sto.Sections(s.ctx, s.user)
	c.Check(err, IsNil)
	c.Check(sections, DeepEquals, []string{"featured", "database"})
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestSectionsQueryTooMany(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", sectionsPath)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		switch n {
		case 0:
			// All good.
		default:
			c.Fatalf("what? %d", n)
		}

		w.WriteHeader(429)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	sections, err := sto.Sections(s.ctx, s.user)
	c.Check(err, Equals, store.ErrTooManyRequests)
	c.Check(sections, IsNil)
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestSectionsQueryCustomStore(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", sectionsPath)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		switch n {
		case 0:
			// All good.
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(200)
		io.WriteString(w, MockSectionsJSON)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device, storeID: "my-brand-store"}
	sto := store.New(&cfg, dauthCtx)

	sections, err := sto.Sections(s.ctx, s.user)
	c.Check(err, IsNil)
	c.Check(sections, DeepEquals, []string{"featured", "database"})
}

func (s *storeTestSuite) TestSectionsQueryErrors(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", sectionsPath)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		w.WriteHeader(500)
		io.WriteString(w, "very unhappy")
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	_, err := sto.Sections(s.ctx, s.user)
	c.Assert(err, ErrorMatches, `cannot retrieve sections: got unexpected HTTP status code 500 via GET to.*`)
}

/*
	acquired via:

curl -s -H "accept: application/json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://api.snapcraft.io/v2/snaps/categories'
*/
const MockCategoriesJSON = `{
    "categories": [
        {
            "name": "featured"
        },
        {
            "name": "database"
        }
    ]
}
`

func (s *storeTestSuite) TestCategoriesQuery(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", categoriesPath)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		switch n {
		case 0:
			// All good.
		default:
			c.Fatalf("unexpected request to %q", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, MockCategoriesJSON)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	categories, err := sto.Categories(s.ctx, s.user)
	c.Check(err, IsNil)
	c.Check(categories, DeepEquals, []store.CategoryDetails{{Name: "featured"}, {Name: "database"}})
	c.Check(n, Equals, 1)
}

const mockNamesJSON = `
{
  "_embedded": {
    "clickindex:package": [
      {
        "aliases": [
          {
            "name": "potato",
            "target": "baz"
          },
          {
            "name": "meh",
            "target": "baz"
          }
        ],
        "apps": ["baz"],
        "title": "a title",
        "summary": "oneary plus twoary",
        "package_name": "bar",
        "version": "2.0"
      },
      {
        "aliases": [{"name": "meh", "target": "foo"}],
        "apps": ["foo"],
        "package_name": "foo",
        "version": "1.0"
      }
    ]
  }
}`

func (s *storeTestSuite) TestSnapCommandsOnClassic(c *C) {
	s.testSnapCommands(c, true)
}

func (s *storeTestSuite) TestSnapCommandsOnCore(c *C) {
	s.testSnapCommands(c, false)
}

func (s *storeTestSuite) testSnapCommands(c *C, onClassic bool) {
	c.Assert(os.MkdirAll(dirs.SnapCacheDir, 0755), IsNil)
	defer release.MockOnClassic(onClassic)()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		switch n {
		case 0:
			query := r.URL.Query()
			c.Check(query, HasLen, 1)
			expectedConfinement := "strict"
			if onClassic {
				expectedConfinement = "strict,classic"
			}
			c.Check(query.Get("confinement"), Equals, expectedConfinement)
			c.Check(r.URL.Path, Equals, "/api/v1/snaps/names")
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.Header().Set("Content-Length", fmt.Sprint(len(mockNamesJSON)))
		w.WriteHeader(200)
		io.WriteString(w, mockNamesJSON)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{StoreBaseURL: serverURL}, dauthCtx)

	db, err := advisor.Create()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt support is disabled")
	}
	c.Assert(err, IsNil)
	defer db.Rollback()

	var bufNames bytes.Buffer
	err = sto.WriteCatalogs(s.ctx, &bufNames, db)
	c.Assert(err, IsNil)
	db.Commit()
	c.Check(bufNames.String(), Equals, "bar\nfoo\n")

	dump, err := advisor.DumpCommands()
	c.Assert(err, IsNil)
	c.Check(dump, DeepEquals, map[string]string{
		"foo":     `[{"snap":"foo","version":"1.0"}]`,
		"bar.baz": `[{"snap":"bar","version":"2.0"}]`,
		"potato":  `[{"snap":"bar","version":"2.0"}]`,
		"meh":     `[{"snap":"bar","version":"2.0"},{"snap":"foo","version":"1.0"}]`,
	})
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestSnapCommandsTooMany(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapCacheDir, 0755), IsNil)

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/api/v1/snaps/names")
		default:
			c.Fatalf("what? %d", n)
		}

		w.WriteHeader(429)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{StoreBaseURL: serverURL}, dauthCtx)

	db, err := advisor.Create()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt support is disabled")
	}
	c.Assert(err, IsNil)
	defer db.Rollback()

	var bufNames bytes.Buffer
	err = sto.WriteCatalogs(s.ctx, &bufNames, db)
	c.Assert(err, Equals, store.ErrTooManyRequests)
	db.Commit()
	c.Check(bufNames.String(), Equals, "")

	dump, err := advisor.DumpCommands()
	c.Assert(err, IsNil)
	c.Check(dump, HasLen, 0)
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) testFind(c *C, apiV1 bool) {
	restore := release.MockOnClassic(false)
	defer restore()

	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			v2Hit = true
			assertRequest(c, r, "GET", findPath)
		}
		query := r.URL.Query()

		q := query.Get("q")
		c.Check(q, Equals, "hello")

		c.Check(r.UserAgent(), Equals, userAgent)

		if apiV1 {
			// check device authorization is set, implicitly checking doRequest was used
			c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

			// no store ID by default
			storeID := r.Header.Get("X-Ubuntu-Store")
			c.Check(storeID, Equals, "")

			c.Check(r.URL.Query().Get("fields"), Equals, "abc,def")

			c.Check(r.Header.Get("X-Ubuntu-Series"), Equals, release.Series)
			c.Check(r.Header.Get("X-Ubuntu-Architecture"), Equals, arch.DpkgArchitecture())
			c.Check(r.Header.Get("X-Ubuntu-Classic"), Equals, "false")

			c.Check(r.Header.Get("X-Ubuntu-Confinement"), Equals, "")

			w.Header().Set("X-Suggested-Currency", "GBP")

			w.Header().Set("Content-Type", "application/hal+json")
			w.WriteHeader(200)

			io.WriteString(w, mockSearchJSON)
		} else {

			// check device authorization is set, implicitly checking doRequest was used
			c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

			// no store ID by default
			storeID := r.Header.Get("Snap-Device-Store")
			c.Check(storeID, Equals, "")

			c.Check(r.URL.Query().Get("fields"), Equals, "abc,def")

			c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
			c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.DpkgArchitecture())
			c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

			w.Header().Set("X-Suggested-Currency", "GBP")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)

			io.WriteString(w, mockSearchJSONv2)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
		FindFields:   []string{"abc", "def"},
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	snaps, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	snp := snaps[0]
	c.Check(snp.InstanceName(), Equals, "hello-world")
	c.Check(snp.Revision, Equals, snap.R(27))
	c.Check(snp.SnapID, Equals, helloWorldSnapID)
	c.Check(snp.Publisher, Equals, snap.StoreAccount{
		ID:          "canonical",
		Username:    "canonical",
		DisplayName: "Canonical",
		Validation:  "verified",
	})
	c.Check(snp.Version, Equals, "6.3")
	c.Check(snp.Size, Equals, int64(20480))
	c.Check(snp.Channel, Equals, "stable")
	c.Check(snp.Description(), Equals, "This is a simple hello world example.")
	c.Check(snp.Summary(), Equals, "The 'hello-world' of snaps")
	c.Check(snp.Title(), Equals, "This Is The Most Fantastical Snap of He…")
	c.Check(snp.License, Equals, "MIT")
	// this is more a "we know this isn't there" than an actual test for a wanted feature
	// NOTE snap.Epoch{} (which prints as "0", and is thus Unset) is not a valid Epoch.
	c.Check(snp.Epoch, DeepEquals, snap.Epoch{})
	c.Assert(snp.Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Assert(snp.Paid, Equals, true)
	c.Assert(snp.Media, DeepEquals, snap.MediaInfos{
		{
			Type: "icon",
			URL:  "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
		}, {
			Type: "screenshot",
			URL:  "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png",
		},
	})
	c.Check(snp.MustBuy, Equals, true)
	c.Check(snp.Contact(), Equals, "mailto:snaps@canonical.com")
	c.Check(snp.Base, Equals, "bare-base")

	// Make sure the epoch (currently not sent by the store) defaults to "0"
	c.Check(snp.Epoch.String(), Equals, "0")

	c.Check(sto.SuggestedCurrency(), Equals, "GBP")

	if apiV1 {
		c.Check(snp.Architectures, DeepEquals, []string{"all"})
		c.Check(snp.Sha3_384, Matches, `[[:xdigit:]]{96}`)
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(snp.Links(), DeepEquals, map[string][]string{
			"contact": {"mailto:snaps@canonical.com"},
			"website": {"https://ubuntu.com"},
		})
		c.Check(snp.Website(), Equals, "https://ubuntu.com")
		c.Check(snp.StoreURL, Equals, "https://snapcraft.io/hello-world")
		c.Check(snp.CommonIDs, DeepEquals, []string{"aaa", "bbb"})
		c.Check(snp.Categories, DeepEquals, []snap.CategoryInfo{
			{
				Featured: true,
				Name:     "featured",
			}, {
				Featured: false,
				Name:     "productivity",
			},
		})
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1(c *C) {
	apiV1 := true
	s.testFind(c, apiV1)
}

func (s *storeTestSuite) TestFindV2(c *C) {
	s.testFind(c, false)
}

func (s *storeTestSuite) TestFindV2FindFields(c *C) {
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(nil, dauthCtx)

	findFields := sto.FindFields()
	sort.Strings(findFields)
	c.Assert(findFields, DeepEquals, []string{
		"base", "categories", "channel", "common-ids", "confinement", "contact",
		"description", "download", "license", "links", "media", "prices", "private",
		"publisher", "revision", "store-url", "summary", "title", "type",
		"version", "website"})
}

func (s *storeTestSuite) testFindPrivate(c *C, apiV1 bool) {
	n := 0
	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			v2Hit = true
			assertRequest(c, r, "GET", findPath)
		}

		query := r.URL.Query()
		name := query.Get("name")
		q := query.Get("q")

		switch n {
		case 0:
			if apiV1 {
				c.Check(r.URL.Path, Matches, ".*/search")
			} else {
				c.Check(r.URL.Path, Matches, ".*/find")
			}
			c.Check(name, Equals, "")
			c.Check(q, Equals, "foo")
			c.Check(query.Get("private"), Equals, "true")
		case 1:
			if apiV1 {
				c.Check(r.URL.Path, Matches, ".*/search")
			} else {
				c.Check(r.URL.Path, Matches, ".*/find")
			}
			c.Check(name, Equals, "foo")
			c.Check(q, Equals, "")
			c.Check(query.Get("private"), Equals, "true")
		default:
			c.Fatalf("what? %d", n)
		}

		if apiV1 {
			w.Header().Set("Content-Type", "application/hal+json")
			w.WriteHeader(200)
			io.WriteString(w, strings.Replace(mockSearchJSON, `"EUR": 2.99, "USD": 3.49`, "", -1))

		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			io.WriteString(w, strings.Replace(mockSearchJSON, `"EUR": "2.99", "USD": "3.49"`, "", -1))
		}

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}

	sto := store.New(&cfg, nil)

	_, err := sto.Find(s.ctx, &store.Search{Query: "foo", Private: true}, s.user)
	c.Check(err, IsNil)

	_, err = sto.Find(s.ctx, &store.Search{Query: "foo", Prefix: true, Private: true}, s.user)
	c.Check(err, IsNil)

	_, err = sto.Find(s.ctx, &store.Search{Query: "foo", Private: true}, nil)
	c.Check(err, Equals, store.ErrUnauthenticated)

	_, err = sto.Find(s.ctx, &store.Search{Query: "name:foo", Private: true}, s.user)
	c.Check(err, Equals, store.ErrBadQuery)

	c.Check(n, Equals, 2)

	if apiV1 {
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1Private(c *C) {
	apiV1 := true
	s.testFindPrivate(c, apiV1)
}

func (s *storeTestSuite) TestFindV2Private(c *C) {
	s.testFindPrivate(c, false)
}

func (s *storeTestSuite) TestFindV2ErrorList(c *C) {
	const errJSON = `{
		"error-list": [
			{
				"code": "api-error",
				"message": "api error occurred"
			}
		]
	}`
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", findPath)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, errJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)
	_, err := sto.Find(s.ctx, &store.Search{Query: "x"}, nil)
	c.Check(err, ErrorMatches, `api error occurred`)
}

func (s *storeTestSuite) TestFindFailures(c *C) {
	// bad query check is done early in Find(), so the test covers both search
	// v1 & v2
	sto := store.New(&store.Config{StoreBaseURL: new(url.URL)}, nil)
	_, err := sto.Find(s.ctx, &store.Search{Query: "foo:bar"}, nil)
	c.Check(err, Equals, store.ErrBadQuery)
}

func (s *storeTestSuite) TestFindInvalidScope(c *C) {
	// bad query check is done early in Find(), so the test covers both search
	// v1 & v2
	sto := store.New(&store.Config{StoreBaseURL: new(url.URL)}, nil)
	_, err := sto.Find(s.ctx, &store.Search{Query: "", Scope: "foo"}, nil)
	c.Check(err, Equals, store.ErrInvalidScope)
}

func (s *storeTestSuite) testFindFails(c *C, apiV1 bool) {
	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			assertRequest(c, r, "GET", findPath)
			v2Hit = true
		}
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		http.Error(w, http.StatusText(418), 418) // I'm a teapot
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `cannot search: got unexpected HTTP status code 418 via GET to "http://\S+[?&]q=hello.*"`)
	c.Check(snaps, HasLen, 0)
	if apiV1 {
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1Fails(c *C) {
	apiV1 := true
	s.testFindFails(c, apiV1)
}

func (s *storeTestSuite) TestFindV2Fails(c *C) {
	s.testFindFails(c, false)
}

func (s *storeTestSuite) testFindBadContentType(c *C, apiV1 bool) {
	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			v2Hit = true
			assertRequest(c, r, "GET", findPath)
		}
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		if apiV1 {
			io.WriteString(w, mockSearchJSON)
		} else {
			io.WriteString(w, mockSearchJSONv2)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `received an unexpected content type \("text/plain[^"]+"\) when trying to search via "http://\S+[?&]q=hello.*"`)
	c.Check(snaps, HasLen, 0)
	if apiV1 {
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1BadContentType(c *C) {
	apiV1 := true
	s.testFindBadContentType(c, apiV1)
}

func (s *storeTestSuite) TestFindV2BadContentType(c *C) {
	s.testFindBadContentType(c, false)
}

func (s *storeTestSuite) testFindBadBody(c *C, apiV1 bool) {
	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			v2Hit = true
			assertRequest(c, r, "GET", findPath)
		}
		query := r.URL.Query()
		c.Check(query.Get("q"), Equals, "hello")
		if apiV1 {
			w.Header().Set("Content-Type", "application/hal+json")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		io.WriteString(w, "<hello>")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `invalid character '<' looking for beginning of value`)
	c.Check(snaps, HasLen, 0)
	if apiV1 {
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1BadBody(c *C) {
	apiV1 := true
	s.testFindBadBody(c, apiV1)
}

func (s *storeTestSuite) TestFindV2BadBody(c *C) {
	s.testFindBadBody(c, false)
}

func (s *storeTestSuite) TestFindV2_404NoFallbackIfNewStore(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(n, Equals, 0)
		n++
		assertRequest(c, r, "GET", findPath)
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		w.Header().Set("Snap-Store-Version", "30")
		w.WriteHeader(404)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `.*got unexpected HTTP status code 404.*`)
	c.Check(n, Equals, 1)
}

// testFindPermanent500 checks that a permanent 500 error on every request
// results in 5 retries, after which the caller gets the 500 status.
func (s *storeTestSuite) testFindPermanent500(c *C, apiV1 bool) {
	var n = 0
	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			v2Hit = true
			assertRequest(c, r, "GET", findPath)
		}
		n++
		w.WriteHeader(500)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `cannot search: got unexpected HTTP status code 500 via GET to "http://\S+[?&]q=hello.*"`)
	c.Assert(n, Equals, 5)
	if apiV1 {
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1Permanent500(c *C) {
	apiV1 := true
	s.testFindPermanent500(c, apiV1)
}

func (s *storeTestSuite) TestFindV2Permanent500(c *C) {
	s.testFindPermanent500(c, false)
}

// testFind500OnceThenSucceed checks that a single 500 failure, followed by
// a successful response is handled.
func (s *storeTestSuite) testFind500OnceThenSucceed(c *C, apiV1 bool) {
	var n = 0
	var v1Fallback, v2Hit bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			v1Fallback = true
			assertRequest(c, r, "GET", searchPath)
		} else {
			v2Hit = true
			assertRequest(c, r, "GET", findPath)
		}
		n++
		if n == 1 {
			w.WriteHeader(500)
		} else {
			if apiV1 {
				w.Header().Set("Content-Type", "application/hal+json")
				w.WriteHeader(200)
				io.WriteString(w, strings.Replace(mockSearchJSON, `"EUR": 2.99, "USD": 3.49`, "", -1))
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				io.WriteString(w, strings.Replace(mockSearchJSONv2, `"EUR": "2.99", "USD": "3.49"`, "", -1))
			}
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
		FindFields:   []string{},
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(s.ctx, &store.Search{Query: "hello"}, nil)
	c.Check(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Assert(n, Equals, 2)
	if apiV1 {
		c.Check(v1Fallback, Equals, true)
	} else {
		c.Check(v2Hit, Equals, true)
	}
}

func (s *storeTestSuite) TestFindV1_500Once(c *C) {
	apiV1 := true
	s.testFind500OnceThenSucceed(c, apiV1)
}

func (s *storeTestSuite) TestFindV2_500Once(c *C) {
	s.testFind500OnceThenSucceed(c, false)
}

func (s *storeTestSuite) testFindAuthFailed(c *C, apiV1 bool) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
		}
		switch r.URL.Path {
		case searchPath:
			c.Assert(apiV1, Equals, true)
			fallthrough
		case findPath:
			// check authorization is set
			authorization := r.Header.Get("Authorization")
			c.Check(authorization, Equals, expectedAuthorization(c, s.user))

			query := r.URL.Query()
			c.Check(query.Get("q"), Equals, "foo")
			if release.OnClassic {
				c.Check(query.Get("confinement"), Matches, `strict,classic|classic,strict`)
			} else {
				c.Check(query.Get("confinement"), Equals, "strict")
			}
			if apiV1 {
				w.Header().Set("Content-Type", "application/hal+json")
				io.WriteString(w, mockSearchJSON)
			} else {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, mockSearchJSONv2)
			}
		case ordersPath:
			c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
			c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
			c.Check(r.URL.Path, Equals, ordersPath)
			w.WriteHeader(401)
			io.WriteString(w, "{}")
		default:
			c.Fatalf("unexpected query %s %s", r.Method, r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(s.ctx, &store.Search{Query: "foo"}, s.user)
	c.Assert(err, IsNil)

	// Check that we log an error.
	c.Check(s.logbuf.String(), Matches, "(?ms).* cannot get user orders: invalid credentials")

	// But still successfully return snap information.
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].SnapID, Equals, helloWorldSnapID)
	c.Check(snaps[0].Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Check(snaps[0].MustBuy, Equals, true)
}

func (s *storeTestSuite) TestFindV1AuthFailed(c *C) {
	apiV1 := true
	s.testFindAuthFailed(c, apiV1)
}

func (s *storeTestSuite) TestFindV2AuthFailed(c *C) {
	s.testFindAuthFailed(c, false)
}

func (s *storeTestSuite) testFindCommonIDs(c *C, apiV1 bool) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			assertRequest(c, r, "GET", searchPath)
		} else {
			assertRequest(c, r, "GET", findPath)
		}
		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")

		switch n {
		case 0:
			if apiV1 {
				c.Check(r.URL.Path, Matches, ".*/search")
			} else {
				c.Check(r.URL.Path, Matches, ".*/find")
			}
			c.Check(name, Equals, "")
			c.Check(q, Equals, "foo")
		default:
			c.Fatalf("what? %d", n)
		}

		if apiV1 {
			w.Header().Set("Content-Type", "application/hal+json")
			w.WriteHeader(200)
			io.WriteString(w, strings.Replace(mockSearchJSON,
				`"common_ids": []`,
				`"common_ids": ["org.hello"]`, -1))
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			io.WriteString(w, mockSearchJSONv2)
		}

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	sto := store.New(&cfg, nil)

	infos, err := sto.Find(s.ctx, &store.Search{Query: "foo"}, nil)
	c.Check(err, IsNil)
	c.Assert(infos, HasLen, 1)
	if apiV1 {
		c.Check(infos[0].CommonIDs, DeepEquals, []string{"org.hello"})
	} else {
		c.Check(infos[0].CommonIDs, DeepEquals, []string{"aaa", "bbb"})
	}
}

func (s *storeTestSuite) TestFindV1CommonIDs(c *C) {
	apiV1 := true
	s.testFindCommonIDs(c, apiV1)
}

func (s *storeTestSuite) TestFindV2CommonIDs(c *C) {
	s.testFindCommonIDs(c, false)
}

func (s *storeTestSuite) testFindByCommonID(c *C, apiV1 bool) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiV1 {
			if strings.Contains(r.URL.Path, findPath) {
				forceSearchV1(w)
				return
			}
			assertRequest(c, r, "GET", searchPath)
		} else {
			assertRequest(c, r, "GET", findPath)
		}
		query := r.URL.Query()

		switch n {
		case 0:
			if apiV1 {
				c.Check(r.URL.Path, Matches, ".*/search")
				c.Check(query["common_id"], DeepEquals, []string{"org.hello"})
			} else {
				c.Check(r.URL.Path, Matches, ".*/find")
				c.Check(query["common-id"], DeepEquals, []string{"org.hello"})
			}
			c.Check(query["name"], IsNil)
			c.Check(query["q"], IsNil)
		default:
			c.Fatalf("expected 1 query, now on %d", n+1)
		}

		if apiV1 {
			w.Header().Set("Content-Type", "application/hal+json")
			w.WriteHeader(200)
			io.WriteString(w, strings.Replace(mockSearchJSON,
				`"common_ids": []`,
				`"common_ids": ["org.hello"]`, -1))
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			io.WriteString(w, mockSearchJSONv2)
		}

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	sto := store.New(&cfg, nil)

	infos, err := sto.Find(s.ctx, &store.Search{CommonID: "org.hello"}, nil)
	c.Check(err, IsNil)
	c.Assert(infos, HasLen, 1)
	if apiV1 {
		c.Check(infos[0].CommonIDs, DeepEquals, []string{"org.hello"})
	} else {
		c.Check(infos[0].CommonIDs, DeepEquals, []string{"aaa", "bbb"})
	}
}

func (s *storeTestSuite) TestFindV1ByCommonID(c *C) {
	apiV1 := true
	s.testFindByCommonID(c, apiV1)
}

func (s *storeTestSuite) TestFindV2ByCommonID(c *C) {
	s.testFindByCommonID(c, false)
}

func (s *storeTestSuite) TestFindClientUserAgent(c *C) {
	clientUserAgent := "some-client/1.0"

	serverWasHit := false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-Client-User-Agent"), Equals, clientUserAgent)
		serverWasHit = true

		http.Error(w, http.StatusText(418), 418) // I'm a teapot
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}

	req, err := http.NewRequest("GET", "/", nil)
	c.Assert(err, IsNil)
	req.Header.Add("User-Agent", clientUserAgent)
	ctx := store.WithClientUserAgent(s.ctx, req)

	sto := store.New(&cfg, nil)
	sto.Find(ctx, &store.Search{Query: "hello"}, nil)
	c.Assert(serverWasHit, Equals, true)
}

func (s *storeTestSuite) TestAuthLocationDependsOnEnviron(c *C) {
	defer snapdenv.MockUseStagingStore(false)()
	before := store.AuthLocation()

	snapdenv.MockUseStagingStore(true)
	after := store.AuthLocation()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestAuthURLDependsOnEnviron(c *C) {
	defer snapdenv.MockUseStagingStore(false)()
	before := store.AuthURL()

	snapdenv.MockUseStagingStore(true)
	after := store.AuthURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestApiURLDependsOnEnviron(c *C) {
	defer snapdenv.MockUseStagingStore(false)()
	before := store.ApiURL()

	snapdenv.MockUseStagingStore(true)
	after := store.ApiURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestStoreURLDependsOnEnviron(c *C) {
	// This also depends on the API URL, but that's tested separately (see
	// TestApiURLDependsOnEnviron).
	api := store.ApiURL()

	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", ""), IsNil)
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", ""), IsNil)

	// Test in order of precedence (low first) leaving env vars set as we go ...

	u, err := store.StoreURL(api)
	c.Assert(err, IsNil)
	c.Check(u.String(), Matches, api.String()+".*")

	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "https://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")
	u, err = store.StoreURL(api)
	c.Assert(err, IsNil)
	c.Check(u.String(), Matches, "https://force-api.local/.*")

	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", "https://force-cpi.local/api/v1/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_CPI_URL", "")
	u, err = store.StoreURL(api)
	c.Assert(err, IsNil)
	c.Check(u.String(), Matches, "https://force-cpi.local/.*")
}

func (s *storeTestSuite) TestStoreURLBadEnvironAPI(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")
	_, err := store.StoreURL(store.ApiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse \"?://force-api.local/\"?: missing protocol scheme")
}

func (s *storeTestSuite) TestStoreURLBadEnvironCPI(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", "://force-cpi.local/api/v1/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_CPI_URL", "")
	_, err := store.StoreURL(store.ApiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_CPI_URL: parse \"?://force-cpi.local/\"?: missing protocol scheme")
}

func (s *storeTestSuite) TestStoreDeveloperURLDependsOnEnviron(c *C) {
	defer snapdenv.MockUseStagingStore(false)()
	before := store.StoreDeveloperURL()

	snapdenv.MockUseStagingStore(true)
	after := store.StoreDeveloperURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestStoreDefaultConfig(c *C) {
	c.Check(store.DefaultConfig().StoreBaseURL.String(), Equals, "https://api.snapcraft.io/")
	c.Check(store.DefaultConfig().AssertionsBaseURL, IsNil)
}

func (s *storeTestSuite) TestNew(c *C) {
	aStore := store.New(nil, nil)
	c.Assert(aStore, NotNil)
	// check for fields
	c.Check(aStore.DetailFields(), DeepEquals, store.DefaultConfig().DetailFields)
}

func (s *storeTestSuite) TestSuggestedCurrency(c *C) {
	suggestedCurrency := "GBP"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", infoPathPattern)
		w.Header().Set("X-Suggested-Currency", suggestedCurrency)
		w.WriteHeader(200)

		io.WriteString(w, mockInfoJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	// the store doesn't know the currency until after the first search, so fall back to dollars
	c.Check(sto.SuggestedCurrency(), Equals, "USD")

	// we should soon have a suggested currency
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(sto.SuggestedCurrency(), Equals, "GBP")

	suggestedCurrency = "EUR"

	// checking the currency updates
	result, err = sto.SnapInfo(s.ctx, spec, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(sto.SuggestedCurrency(), Equals, "EUR")
}

func (s *storeTestSuite) TestDecorateOrders(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", ordersPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockOrdersJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, dauthCtx)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}
	funkyApp.Paid = true

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}
	otherApp.Paid = true

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, IsNil)

	c.Check(helloWorld.MustBuy, Equals, false)
	c.Check(funkyApp.MustBuy, Equals, false)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersFailedAccess(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", ordersPath)
		c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(401)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}
	funkyApp.Paid = true

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}
	otherApp.Paid = true

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, NotNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersNoAuth(c *C) {
	cfg := store.Config{}
	sto := store.New(&cfg, nil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}
	funkyApp.Paid = true

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}
	otherApp.Paid = true

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err := sto.DecorateOrders(snaps, nil)
	c.Assert(err, IsNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersAllFree(c *C) {
	requestRecieved := false

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Error(r.URL.Path)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		requestRecieved = true
		io.WriteString(w, `{"orders": []}`)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	sto := store.New(&cfg, nil)

	// This snap is free
	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	// This snap is also free
	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID

	snaps := []*snap.Info{helloWorld, funkyApp}

	// There should be no request to the purchase server.
	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, IsNil)
	c.Check(requestRecieved, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersSingle(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockSingleOrderJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, dauthCtx)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	snaps := []*snap.Info{helloWorld}

	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersSingleFreeSnap(c *C) {
	cfg := store.Config{}
	sto := store.New(&cfg, nil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	snaps := []*snap.Info{helloWorld}

	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersSingleNotFound(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", ordersPath)
		c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(404)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, dauthCtx)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	snaps := []*snap.Info{helloWorld}

	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (s *storeTestSuite) TestDecorateOrdersTokenExpired(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(401)
		io.WriteString(w, "")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, dauthCtx)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	snaps := []*snap.Info{helloWorld}

	err := sto.DecorateOrders(snaps, s.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (s *storeTestSuite) TestMustBuy(c *C) {
	// Never need to buy a free snap.
	c.Check(store.MustBuy(false, true), Equals, false)
	c.Check(store.MustBuy(false, false), Equals, false)

	// Don't need to buy snaps that have been bought.
	c.Check(store.MustBuy(true, true), Equals, false)

	// Need to buy snaps that aren't bought.
	c.Check(store.MustBuy(true, false), Equals, true)
}

var buyTests = []struct {
	suggestedCurrency string
	expectedInput     string
	buyStatus         int
	buyResponse       string
	buyErrorMessage   string
	buyErrorCode      string
	snapID            string
	price             float64
	currency          string
	expectedResult    *client.BuyResult
	expectedError     string
}{
	{
		// successful buying
		suggestedCurrency: "EUR",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"0.99","currency":"EUR"}`,
		buyResponse:       mockOrderResponseJSON,
		expectedResult:    &client.BuyResult{State: "Complete"},
	},
	{
		// failure due to invalid price
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"5.99","currency":"USD"}`,
		buyStatus:         400,
		buyErrorCode:      "invalid-field",
		buyErrorMessage:   "invalid price specified",
		price:             5.99,
		expectedError:     "cannot buy snap: bad request: invalid price specified",
	},
	{
		// failure due to unknown snap ID
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"invalid snap ID","amount":"0.99","currency":"EUR"}`,
		buyStatus:         404,
		buyErrorCode:      "not-found",
		buyErrorMessage:   "Snap package not found",
		snapID:            "invalid snap ID",
		price:             0.99,
		currency:          "EUR",
		expectedError:     "cannot buy snap: server says not found: Snap package not found",
	},
	{
		// failure due to "Purchase failed"
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"1.23","currency":"USD"}`,
		buyStatus:         402, // Payment Required
		buyErrorCode:      "request-failed",
		buyErrorMessage:   "Purchase failed",
		expectedError:     "payment declined",
	},
	{
		// failure due to no payment methods
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"1.23","currency":"USD"}`,
		buyStatus:         403,
		buyErrorCode:      "no-payment-methods",
		buyErrorMessage:   "No payment methods associated with your account.",
		expectedError:     "no payment methods",
	},
	{
		// failure due to terms of service not accepted
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"1.23","currency":"USD"}`,
		buyStatus:         403,
		buyErrorCode:      "tos-not-accepted",
		buyErrorMessage:   "You must accept the latest terms of service first.",
		expectedError:     "terms of service not accepted",
	},
}

func (s *storeTestSuite) TestBuy500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case detailsPath("hello-world"):
			n++
			w.WriteHeader(500)
		case buyPath:
		case customersMePath:
			// default 200 response
		default:
			c.Fatalf("unexpected query %s %s", r.Method, r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, dauthCtx)

	buyOptions := &client.BuyOptions{
		SnapID:   helloWorldSnapID,
		Currency: "USD",
		Price:    1,
	}
	_, err := sto.Buy(buyOptions, s.user)
	c.Assert(err, NotNil)
}

func (s *storeTestSuite) TestBuy(c *C) {
	for _, test := range buyTests {
		searchServerCalled := false
		purchaseServerGetCalled := false
		purchaseServerPostCalled := false
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case infoPath("hello-world"):
				c.Assert(r.Method, Equals, "GET")
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Suggested-Currency", test.suggestedCurrency)
				w.WriteHeader(200)
				io.WriteString(w, mockInfoJSON)
				searchServerCalled = true
			case ordersPath:
				c.Assert(r.Method, Equals, "GET")
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
				c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
				io.WriteString(w, `{"orders": []}`)
				purchaseServerGetCalled = true
			case buyPath:
				c.Assert(r.Method, Equals, "POST")
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
				c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
				c.Check(r.Header.Get("Content-Type"), Equals, store.JsonContentType)
				c.Check(r.URL.Path, Equals, buyPath)
				jsonReq, err := ioutil.ReadAll(r.Body)
				c.Assert(err, IsNil)
				c.Check(string(jsonReq), Equals, test.expectedInput)
				if test.buyErrorCode == "" {
					io.WriteString(w, test.buyResponse)
				} else {
					w.WriteHeader(test.buyStatus)
					// TODO(matt): this is fugly!
					fmt.Fprintf(w, `
{
	"error_list": [
		{
			"code": "%s",
			"message": "%s"
		}
	]
}`, test.buyErrorCode, test.buyErrorMessage)
				}

				purchaseServerPostCalled = true
			default:
				c.Fatalf("unexpected query %s %s", r.Method, r.URL.Path)
			}
		}))
		c.Assert(mockServer, NotNil)
		defer mockServer.Close()

		mockServerURL, _ := url.Parse(mockServer.URL)
		dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
		cfg := store.Config{
			StoreBaseURL: mockServerURL,
		}
		sto := store.New(&cfg, dauthCtx)

		// Find the snap first
		spec := store.SnapSpec{
			Name: "hello-world",
		}
		snap, err := sto.SnapInfo(s.ctx, spec, s.user)
		c.Assert(snap, NotNil)
		c.Assert(err, IsNil)

		buyOptions := &client.BuyOptions{
			SnapID:   snap.SnapID,
			Currency: sto.SuggestedCurrency(),
			Price:    snap.Prices[sto.SuggestedCurrency()],
		}
		if test.snapID != "" {
			buyOptions.SnapID = test.snapID
		}
		if test.currency != "" {
			buyOptions.Currency = test.currency
		}
		if test.price > 0 {
			buyOptions.Price = test.price
		}
		result, err := sto.Buy(buyOptions, s.user)

		c.Check(result, DeepEquals, test.expectedResult)
		if test.expectedError == "" {
			c.Check(err, IsNil)
		} else {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, test.expectedError)
		}

		c.Check(searchServerCalled, Equals, true)
		c.Check(purchaseServerGetCalled, Equals, true)
		c.Check(purchaseServerPostCalled, Equals, true)
	}
}

func (s *storeTestSuite) TestBuyFailArgumentChecking(c *C) {
	sto := store.New(&store.Config{}, nil)

	// no snap ID
	result, err := sto.Buy(&client.BuyOptions{
		Price:    1.0,
		Currency: "USD",
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: snap ID missing")

	// no price
	result, err = sto.Buy(&client.BuyOptions{
		SnapID:   "snap ID",
		Currency: "USD",
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: invalid expected price")

	// no currency
	result, err = sto.Buy(&client.BuyOptions{
		SnapID: "snap ID",
		Price:  1.0,
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: currency missing")

	// no user
	result, err = sto.Buy(&client.BuyOptions{
		SnapID:   "snap ID",
		Price:    1.0,
		Currency: "USD",
	}, nil)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "you need to log in first")
}

var readyToBuyTests = []struct {
	Input      func(w http.ResponseWriter)
	Test       func(c *C, err error)
	NumOfCalls int
}{
	{
		// A user account the is ready for buying
		Input: func(w http.ResponseWriter) {
			io.WriteString(w, `
{
  "latest_tos_date": "2016-09-14T00:00:00+00:00",
  "accepted_tos_date": "2016-09-14T15:56:49+00:00",
  "latest_tos_accepted": true,
  "has_payment_method": true
}
`)
		},
		Test: func(c *C, err error) {
			c.Check(err, IsNil)
		},
		NumOfCalls: 1,
	},
	{
		// A user account that hasn't accepted the TOS
		Input: func(w http.ResponseWriter) {
			io.WriteString(w, `
{
  "latest_tos_date": "2016-10-14T00:00:00+00:00",
  "accepted_tos_date": "2016-09-14T15:56:49+00:00",
  "latest_tos_accepted": false,
  "has_payment_method": true
}
`)
		},
		Test: func(c *C, err error) {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, "terms of service not accepted")
		},
		NumOfCalls: 1,
	},
	{
		// A user account that has no payment method
		Input: func(w http.ResponseWriter) {
			io.WriteString(w, `
{
  "latest_tos_date": "2016-10-14T00:00:00+00:00",
  "accepted_tos_date": "2016-09-14T15:56:49+00:00",
  "latest_tos_accepted": true,
  "has_payment_method": false
}
`)
		},
		Test: func(c *C, err error) {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, "no payment methods")
		},
		NumOfCalls: 1,
	},
	{
		// A user account that has no payment method and has not accepted the TOS
		Input: func(w http.ResponseWriter) {
			io.WriteString(w, `
{
  "latest_tos_date": "2016-10-14T00:00:00+00:00",
  "accepted_tos_date": "2016-09-14T15:56:49+00:00",
  "latest_tos_accepted": false,
  "has_payment_method": false
}
`)
		},
		Test: func(c *C, err error) {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, "no payment methods")
		},
		NumOfCalls: 1,
	},
	{
		// No user account exists
		Input: func(w http.ResponseWriter) {
			w.WriteHeader(404)
			io.WriteString(w, "{}")
		},
		Test: func(c *C, err error) {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, "cannot get customer details: server says no account exists")
		},
		NumOfCalls: 1,
	},
	{
		// An unknown set of errors occurs
		Input: func(w http.ResponseWriter) {
			w.WriteHeader(500)
			io.WriteString(w, `
{
	"error_list": [
		{
			"code": "code 1",
			"message": "message 1"
		},
		{
			"code": "code 2",
			"message": "message 2"
		}
	]
}`)
		},
		Test: func(c *C, err error) {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, `message 1`)
		},
		NumOfCalls: 5,
	},
}

func (s *storeTestSuite) TestReadyToBuy(c *C) {
	for _, test := range readyToBuyTests {
		purchaseServerGetCalled := 0
		mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertRequest(c, r, "GET", customersMePath)
			switch r.Method {
			case "GET":
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
				c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
				c.Check(r.URL.Path, Equals, customersMePath)
				test.Input(w)
				purchaseServerGetCalled++
			default:
				c.Error("Unexpected request method: ", r.Method)
			}
		}))

		c.Assert(mockPurchasesServer, NotNil)
		defer mockPurchasesServer.Close()

		mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
		dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
		cfg := store.Config{
			StoreBaseURL: mockServerURL,
		}
		sto := store.New(&cfg, dauthCtx)

		err := sto.ReadyToBuy(s.user)
		test.Test(c, err)
		c.Check(purchaseServerGetCalled, Equals, test.NumOfCalls)
	}
}

func (s *storeTestSuite) TestDoRequestSetRangeHeaderOnRedirect(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			http.Redirect(w, r, r.URL.Path+"-else", 302)
			n++
		case 1:
			c.Check(r.URL.Path, Equals, "/somewhere-else")
			rg := r.Header.Get("Range")
			c.Check(rg, Equals, "bytes=5-")
		default:
			panic("got more than 2 requests in this test")
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	url, err := url.Parse(mockServer.URL + "/somewhere")
	c.Assert(err, IsNil)
	reqOptions := store.NewRequestOptions("GET", url)
	reqOptions.ExtraHeaders = map[string]string{
		"Range": "bytes=5-",
	}

	sto := store.New(&store.Config{}, nil)
	_, err = sto.DoRequest(s.ctx, sto.Client(), reqOptions, s.user)
	c.Assert(err, IsNil)
}

func (s *storeTestSuite) TestConnectivityCheckHappy(c *C) {
	seenPaths := make(map[string]int, 2)
	var mockServerURL *url.URL
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snaps/info/core":
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Query(), DeepEquals, url.Values{"fields": {"download"}, "architecture": {arch.DpkgArchitecture()}})
			u, err := url.Parse("/download/core")
			c.Assert(err, IsNil)
			io.WriteString(w,
				fmt.Sprintf(`{"channel-map": [{"download": {"url": %q}}, {"download": {"url": %q}}, {"download": {"url": %q}}]}`,
					mockServerURL.ResolveReference(u).String(),
					mockServerURL.String()+"/bogus1/",
					mockServerURL.String()+"/bogus2/",
				))
		case "/download/core":
			c.Check(r.Method, Equals, "HEAD")
			w.WriteHeader(200)
		default:
			c.Fatalf("unexpected request: %s", r.URL.String())
			return
		}
		seenPaths[r.URL.Path]++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	mockServerURL, _ = url.Parse(mockServer.URL)

	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, nil)
	connectivity, err := sto.ConnectivityCheck()
	c.Assert(err, IsNil)
	// everything is the test server, here
	c.Check(connectivity, DeepEquals, map[string]bool{
		mockServerURL.Host: true,
	})
	c.Check(seenPaths, DeepEquals, map[string]int{
		"/v2/snaps/info/core": 1,
		"/download/core":      1,
	})
}

func (s *storeTestSuite) TestConnectivityCheckUnhappy(c *C) {
	store.MockConnCheckStrategy(&s.BaseTest, retry.LimitCount(3, retry.Exponential{
		Initial: time.Millisecond,
		Factor:  1.3,
	}))

	seenPaths := make(map[string]int, 2)
	var mockServerURL *url.URL
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snaps/info/core":
			w.WriteHeader(500)
		default:
			c.Fatalf("unexpected request: %s", r.URL.String())
			return
		}
		seenPaths[r.URL.Path]++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	mockServerURL, _ = url.Parse(mockServer.URL)

	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, nil)
	connectivity, err := sto.ConnectivityCheck()
	c.Assert(err, IsNil)
	// everything is the test server, here
	c.Check(connectivity, DeepEquals, map[string]bool{
		mockServerURL.Host: false,
	})
	// three because retries
	c.Check(seenPaths, DeepEquals, map[string]int{
		"/v2/snaps/info/core": 3,
	})
}

func (s *storeTestSuite) TestCreateCohort(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", cohortsPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		dec := json.NewDecoder(r.Body)
		var req struct {
			Snaps []string
		}
		err := dec.Decode(&req)
		c.Assert(err, IsNil)
		c.Check(dec.More(), Equals, false)

		c.Check(req.Snaps, DeepEquals, []string{"foo", "bar"})

		io.WriteString(w, `{
    "cohort-keys": {
        "potato": "U3VwZXIgc2VjcmV0IHN0dWZmIGVuY3J5cHRlZCBoZXJlLg=="
    }
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	cohorts, err := sto.CreateCohorts(s.ctx, []string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Assert(cohorts, DeepEquals, map[string]string{
		"potato": "U3VwZXIgc2VjcmV0IHN0dWZmIGVuY3J5cHRlZCBoZXJlLg==",
	})
}

func (s *storeTestSuite) TestStoreNoAccess(c *C) {
	nowhereURL, err := url.Parse("http://nowhere.invalid")
	c.Assert(err, IsNil)

	dauthCtx := &testDauthContext{storeOffline: true, device: &auth.DeviceState{
		Serial: "serial",
	}}

	sto := store.New(&store.Config{
		StoreBaseURL: nowhereURL,
	}, dauthCtx)

	_, err = sto.Categories(s.ctx, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.ConnectivityCheck()
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.CreateCohorts(s.ctx, nil)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	err = sto.Download(s.ctx, "name", c.MkDir(), nil, nil, s.user, nil)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	err = sto.DownloadAssertions([]string{nowhereURL.String()}, nil, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, _, err = sto.DownloadStream(s.ctx, "name", nil, 0, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	err = sto.EnsureDeviceSession()
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.Find(s.ctx, &store.Search{Query: "foo", Private: true}, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, _, err = sto.LoginUser("username", "password", "otp")
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	err = sto.ReadyToBuy(s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.Sections(s.ctx, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.SeqFormingAssertion(asserts.RepairType, nil, 0, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, _, err = sto.SnapExists(s.ctx, store.SnapSpec{Name: "snap"}, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, _, err = sto.SnapAction(s.ctx, nil, []*store.SnapAction{{
		Action:       "download",
		InstanceName: "example",
		Channel:      "stable",
	}}, nil, s.user, nil)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.SnapInfo(s.ctx, store.SnapSpec{Name: "snap"}, s.user)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	_, err = sto.UserInfo("me@example.com")
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)

	err = sto.WriteCatalogs(s.ctx, io.Discard, nil)
	c.Check(err, testutil.ErrorIs, store.ErrStoreOffline)
}

func (s *storeTestSuite) TestStoreNoRetryStoreOffline(c *C) {
	c.Assert(httputil.ShouldRetryError(store.ErrStoreOffline), Equals, false)
}
