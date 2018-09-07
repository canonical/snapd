// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/juju/ratelimit"
	"golang.org/x/crypto/sha3"
	"golang.org/x/net/context"
	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

func TestStore(t *testing.T) { TestingT(t) }

type configTestSuite struct{}

var _ = Suite(&configTestSuite{})

func (suite *configTestSuite) TestSetBaseURL(c *C) {
	// Sanity check to prove at least one URI changes.
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
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://example.com: missing protocol scheme")
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
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_SAS_URL: parse ://example.com: missing protocol scheme")
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
	snapActionPath  = "/v2/snaps/refresh"
	infoPathPattern = "/v2/snaps/info/.*"
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

type storeTestSuite struct {
	testutil.BaseTest
	store     *store.Store
	logbuf    *bytes.Buffer
	user      *auth.UserState
	localUser *auth.UserState
	device    *auth.DeviceState

	mockXDelta *testutil.MockCmd

	restoreLogger func()
}

var _ = Suite(&storeTestSuite{})

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

type testAuthContext struct {
	c      *C
	device *auth.DeviceState
	user   *auth.UserState

	proxyStoreID  string
	proxyStoreURL *url.URL

	storeID string

	cloudInfo *auth.CloudInfo
}

func (ac *testAuthContext) Device() (*auth.DeviceState, error) {
	freshDevice := auth.DeviceState{}
	if ac.device != nil {
		freshDevice = *ac.device
	}
	return &freshDevice, nil
}

func (ac *testAuthContext) UpdateDeviceAuth(d *auth.DeviceState, newSessionMacaroon string) (*auth.DeviceState, error) {
	ac.c.Assert(d, DeepEquals, ac.device)
	updated := *ac.device
	updated.SessionMacaroon = newSessionMacaroon
	*ac.device = updated
	return &updated, nil
}

func (ac *testAuthContext) UpdateUserAuth(u *auth.UserState, newDischarges []string) (*auth.UserState, error) {
	ac.c.Assert(u, DeepEquals, ac.user)
	updated := *ac.user
	updated.StoreDischarges = newDischarges
	return &updated, nil
}

func (ac *testAuthContext) StoreID(fallback string) (string, error) {
	if ac.storeID != "" {
		return ac.storeID, nil
	}
	return fallback, nil
}

func (ac *testAuthContext) DeviceSessionRequestParams(nonce string) (*auth.DeviceSessionRequestParams, error) {
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

	return &auth.DeviceSessionRequestParams{
		Request: sessReq.(*asserts.DeviceSessionRequest),
		Serial:  serial.(*asserts.Serial),
		Model:   model.(*asserts.Model),
	}, nil
}

func (ac *testAuthContext) ProxyStoreParams(defaultURL *url.URL) (string, *url.URL, error) {
	if ac.proxyStoreID != "" {
		return ac.proxyStoreID, ac.proxyStoreURL, nil
	}
	return "", defaultURL, nil
}

func (ac *testAuthContext) CloudInfo() (*auth.CloudInfo, error) {
	return ac.cloudInfo, nil
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

func (s *storeTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.store = store.New(nil, nil)
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapMountDir, 0755), IsNil)

	os.Setenv("SNAPD_DEBUG", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAPD_DEBUG") })

	s.logbuf, s.restoreLogger = logger.MockLogger()

	root, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	discharge, err := makeTestDischarge()
	c.Assert(err, IsNil)
	s.user, err = createTestUser(1, root, discharge)
	c.Assert(err, IsNil)
	s.localUser = &auth.UserState{
		ID:       11,
		Username: "test-user",
		Macaroon: "snapd-macaroon",
	}
	s.device = createTestDevice()
	s.mockXDelta = testutil.MockCommand(c, "xdelta3", "")

	store.MockDefaultRetryStrategy(&s.BaseTest, retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	)))
}

func (s *storeTestSuite) TearDownTest(c *C) {
	s.mockXDelta.Restore()
	s.restoreLogger()
	s.BaseTest.TearDownTest(c)
}

func (s *storeTestSuite) expectedAuthorization(c *C, user *auth.UserState) string {
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

func (s *storeTestSuite) TestDownloadOK(c *C) {
	expectedContent := []byte("I was downloaded")

	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		c.Check(url, Equals, "anon-url")
		w.Write(expectedContent)
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContent))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeTestSuite) TestDownloadRangeRequest(c *C) {
	partialContentStr := "partial content "
	missingContentStr := "was downloaded"
	expectedContentStr := partialContentStr + missingContentStr

	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		c.Check(resume, Equals, int64(len(partialContentStr)))
		c.Check(url, Equals, "anon-url")
		w.Write([]byte(missingContentStr))
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = "abcdabcd"
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)

	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestResumeOfCompleted(c *C) {
	expectedContentStr := "nothing downloaded"

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = fmt.Sprintf("%x", sha3.Sum384([]byte(expectedContentStr)))
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(expectedContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)

	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestDownloadEOFHandlesResumeHashCorrectly(c *C) {
	n := 0
	var mockServer *httptest.Server

	// our mock download content
	buf := make([]byte, 50000)
	for i := range buf {
		buf[i] = 'x'
	}
	h := crypto.SHA3_384.New()
	io.Copy(h, bytes.NewBuffer(buf))

	// raise an EOF shortly before the end
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 2 {
			w.Header().Add("Content-Length", fmt.Sprintf("%d", len(buf)))
			w.Write(buf[0 : len(buf)-5])
			mockServer.CloseClientConnections()
			return
		}
		w.Write(buf[len(buf)-5:])
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = mockServer.URL
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(targetFn, testutil.FileEquals, buf)
	c.Assert(s.logbuf.String(), Matches, "(?s).*Retrying .* attempt 2, .*")
}

func (s *storeTestSuite) TestDownloadRetryHashErrorIsFullyRetried(c *C) {
	n := 0
	var mockServer *httptest.Server

	// our mock download content
	buf := make([]byte, 50000)
	for i := range buf {
		buf[i] = 'x'
	}
	h := crypto.SHA3_384.New()
	io.Copy(h, bytes.NewBuffer(buf))

	// raise an EOF shortly before the end and send the WRONG content next
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			w.Header().Add("Content-Length", fmt.Sprintf("%d", len(buf)))
			w.Write(buf[0 : len(buf)-5])
			mockServer.CloseClientConnections()
		case 2:
			io.WriteString(w, "yyyyy")
		case 3:
			w.Write(buf)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = mockServer.URL
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)

	c.Assert(targetFn, testutil.FileEquals, buf)

	c.Assert(s.logbuf.String(), Matches, "(?s).*Retrying .* attempt 2, .*")
}

func (s *storeTestSuite) TestResumeOfCompletedRetriedOnHashFailure(c *C) {
	var mockServer *httptest.Server

	// our mock download content
	buf := make([]byte, 50000)
	badbuf := make([]byte, 50000)
	for i := range buf {
		buf[i] = 'x'
		badbuf[i] = 'y'
	}
	h := crypto.SHA3_384.New()
	io.Copy(h, bytes.NewBuffer(buf))

	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = mockServer.URL
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	c.Assert(ioutil.WriteFile(targetFn+".partial", badbuf, 0644), IsNil)
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)

	c.Assert(targetFn, testutil.FileEquals, buf)

	c.Assert(s.logbuf.String(), Matches, "(?s).*sha3-384 mismatch.*")
}

func (s *storeTestSuite) TestDownloadRetryHashErrorIsFullyRetriedOnlyOnce(c *C) {
	n := 0
	var mockServer *httptest.Server

	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "something invalid")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = mockServer.URL
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = "invalid-hash"
	snap.Size = int64(len("something invalid"))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)

	_, ok := err.(store.HashError)
	c.Assert(ok, Equals, true)
	// ensure we only retried once (as these downloads might be big)
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestDownloadRangeRequestRetryOnHashError(c *C) {
	expectedContentStr := "file was downloaded from scratch"
	partialContentStr := "partial content "

	n := 0
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		n++
		if n == 1 {
			// force sha3 error on first download
			c.Check(resume, Equals, int64(len(partialContentStr)))
			return store.NewHashError("foo", "1234", "5678")
		}
		w.Write([]byte(expectedContentStr))
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = ""
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)

	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestDownloadRangeRequestFailOnHashError(c *C) {
	partialContentStr := "partial content "

	n := 0
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		n++
		return store.NewHashError("foo", "1234", "5678")
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = ""
	snap.Size = int64(len(partialContentStr) + 1)

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `sha3-384 mismatch for "foo": got 1234 but expected 5678`)
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestAuthenticatedDownloadDoesNotUseAnonURL(c *C) {
	expectedContent := []byte("I was downloaded")
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, _ *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		// check user is pass and auth url is used
		c.Check(user, Equals, s.user)
		c.Check(url, Equals, "AUTH-URL")

		w.Write(expectedContent)
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContent))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, s.user, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeTestSuite) TestAuthenticatedDeviceDoesNotUseAnonURL(c *C) {
	expectedContent := []byte("I was downloaded")
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		// check auth url is used
		c.Check(url, Equals, "AUTH-URL")

		w.Write(expectedContent)
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContent))

	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&store.Config{}, authContext)

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := sto.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeTestSuite) TestLocalUserDownloadUsesAnonURL(c *C) {
	expectedContentStr := "I was downloaded"
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		c.Check(url, Equals, "anon-url")

		w.Write([]byte(expectedContentStr))
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContentStr))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, s.localUser, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestDownloadFails(c *C) {
	var tmpfile *os.File
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		tmpfile = w.(*os.File)
		return fmt.Errorf("uh, it failed")
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = 1
	// simulate a failed download
	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, ErrorMatches, "uh, it failed")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (s *storeTestSuite) TestDownloadSyncFails(c *C) {
	var tmpfile *os.File
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		tmpfile = w.(*os.File)
		w.Write([]byte("sync will fail"))
		err := tmpfile.Close()
		c.Assert(err, IsNil)
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len("sync will fail"))

	// simulate a failed sync
	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, ErrorMatches, `(sync|fsync:) .*`)
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (s *storeTestSuite) TestActualDownload(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, "")
		n++
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestActualDownloadNoCDN(c *C) {
	os.Setenv("SNAPPY_STORE_NO_CDN", "1")
	defer os.Unsetenv("SNAPPY_STORE_NO_CDN")

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, "none")
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
}

func (s *storeTestSuite) TestActualDownloadFullCloudInfoFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, `cloud-name="aws" region="us-east-1" availability-zone="us-east-1c"`)

		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, &testAuthContext{c: c, device: s.device, cloudInfo: &auth.CloudInfo{Name: "aws", Region: "us-east-1", AvailabilityZone: "us-east-1c"}})

	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
}

func (s *storeTestSuite) TestActualDownloadLessDetailedCloudInfoFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, `cloud-name="openstack" availability-zone="nova"`)

		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, &testAuthContext{c: c, device: s.device, cloudInfo: &auth.CloudInfo{Name: "openstack", Region: "", AvailabilityZone: "nova"}})

	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
}

func (s *storeTestSuite) TestDownloadCancellation(c *C) {
	// the channel used by mock server to request cancellation from the test
	syncCh := make(chan struct{})

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "foo")
		syncCh <- struct{}{}
		io.WriteString(w, "bar")
		time.Sleep(time.Duration(1) * time.Second)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)

	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan string)
	go func() {
		sha3 := ""
		var buf SillyBuffer
		err := store.Download(ctx, "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
		result <- err.Error()
		close(result)
	}()

	<-syncCh
	cancel()

	err := <-result
	c.Check(n, Equals, 1)
	c.Assert(err, Equals, "The download has been cancelled: context canceled")
}

func (s *storeTestSuite) TestActualDownloadRateLimited(c *C) {
	var ratelimitReaderUsed bool
	restore := store.MockRatelimitReader(func(r io.Reader, bucket *ratelimit.Bucket) io.Reader {
		ratelimitReaderUsed = true
		return r
	})
	defer restore()

	canary := "downloaded data"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, canary)
	}))
	defer ts.Close()

	var buf SillyBuffer
	err := store.Download(context.TODO(), "example-name", "", ts.URL, nil, s.store, &buf, 0, nil, &store.DownloadOptions{RateLimit: 1})
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, canary)
	c.Check(ratelimitReaderUsed, Equals, true)
}

type nopeSeeker struct{ io.ReadWriter }

func (nopeSeeker) Seek(int64, int) (int64, error) {
	return -1, errors.New("what is this, quidditch?")
}

func (s *storeTestSuite) TestActualDownloadNonPurchased402(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		// XXX: the server doesn't behave correctly ATM
		// but 401 for paid snaps is the unlikely case so far
		w.WriteHeader(402)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf bytes.Buffer
	err := store.Download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, nopeSeeker{&buf}, -1, nil, nil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "please buy foo before installing it.")
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestActualDownload404(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(404)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	err := store.Download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &store.DownloadError{})
	c.Check(err.(*store.DownloadError).Code, Equals, 404)
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestActualDownload500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	err := store.Download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &store.DownloadError{})
	c.Check(err.(*store.DownloadError).Code, Equals, 500)
	c.Check(n, Equals, 5)
}

func (s *storeTestSuite) TestActualDownload500Once(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(500)
		} else {
			io.WriteString(w, "response-data")
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 2)
}

// SillyBuffer is a ReadWriteSeeker buffer with a limited size for the tests
// (bytes does not implement an ReadWriteSeeker)
type SillyBuffer struct {
	buf [1024]byte
	pos int64
	end int64
}

func NewSillyBufferString(s string) *SillyBuffer {
	sb := &SillyBuffer{
		pos: int64(len(s)),
		end: int64(len(s)),
	}
	copy(sb.buf[0:], []byte(s))
	return sb
}
func (sb *SillyBuffer) Read(b []byte) (n int, err error) {
	if sb.pos >= int64(sb.end) {
		return 0, io.EOF
	}
	n = copy(b, sb.buf[sb.pos:sb.end])
	sb.pos += int64(n)
	return n, nil
}
func (sb *SillyBuffer) Seek(offset int64, whence int) (int64, error) {
	if whence != 0 {
		panic("only io.SeekStart implemented in SillyBuffer")
	}
	if offset < 0 || offset > int64(sb.end) {
		return 0, fmt.Errorf("seek out of bounds: %d", offset)
	}
	sb.pos = offset
	return sb.pos, nil
}
func (sb *SillyBuffer) Write(p []byte) (n int, err error) {
	n = copy(sb.buf[sb.pos:], p)
	sb.pos += int64(n)
	if sb.pos > sb.end {
		sb.end = sb.pos
	}
	return n, nil
}
func (sb *SillyBuffer) String() string {
	return string(sb.buf[0:sb.pos])
}

func (s *storeTestSuite) TestActualDownloadResume(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	buf := NewSillyBufferString("some ")
	// calc the expected hash
	h := crypto.SHA3_384.New()
	h.Write([]byte("some data"))
	sha3 := fmt.Sprintf("%x", h.Sum(nil))
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, buf, int64(len("some ")), nil, nil)
	c.Check(err, IsNil)
	c.Check(buf.String(), Equals, "some data")
	c.Check(n, Equals, 1)
}

func (s *storeTestSuite) TestUseDeltas(c *C) {
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	restore := release.MockOnClassic(false)
	defer restore()
	altPath := c.MkDir()
	origSnapMountDir := dirs.SnapMountDir
	defer func() { dirs.SnapMountDir = origSnapMountDir }()
	dirs.SnapMountDir = c.MkDir()
	exeInCorePath := filepath.Join(dirs.SnapMountDir, "/core/current/usr/bin/xdelta3")
	os.MkdirAll(filepath.Dir(exeInCorePath), 0755)

	scenarios := []struct {
		env       string
		classic   bool
		exeInHost bool
		exeInCore bool

		wantDelta bool
	}{
		{env: "", classic: false, exeInHost: false, exeInCore: false, wantDelta: false},
		{env: "", classic: false, exeInHost: false, exeInCore: true, wantDelta: true},
		{env: "", classic: false, exeInHost: true, exeInCore: false, wantDelta: true},
		{env: "", classic: false, exeInHost: true, exeInCore: true, wantDelta: true},
		{env: "", classic: true, exeInHost: false, exeInCore: false, wantDelta: false},
		{env: "", classic: true, exeInHost: false, exeInCore: true, wantDelta: true},
		{env: "", classic: true, exeInHost: true, exeInCore: false, wantDelta: true},
		{env: "", classic: true, exeInHost: true, exeInCore: true, wantDelta: true},

		{env: "0", classic: false, exeInHost: false, exeInCore: false, wantDelta: false},
		{env: "0", classic: false, exeInHost: false, exeInCore: true, wantDelta: false},
		{env: "0", classic: false, exeInHost: true, exeInCore: false, wantDelta: false},
		{env: "0", classic: false, exeInHost: true, exeInCore: true, wantDelta: false},
		{env: "0", classic: true, exeInHost: false, exeInCore: false, wantDelta: false},
		{env: "0", classic: true, exeInHost: false, exeInCore: true, wantDelta: false},
		{env: "0", classic: true, exeInHost: true, exeInCore: false, wantDelta: false},
		{env: "0", classic: true, exeInHost: true, exeInCore: true, wantDelta: false},

		{env: "1", classic: false, exeInHost: false, exeInCore: false, wantDelta: false},
		{env: "1", classic: false, exeInHost: false, exeInCore: true, wantDelta: true},
		{env: "1", classic: false, exeInHost: true, exeInCore: false, wantDelta: true},
		{env: "1", classic: false, exeInHost: true, exeInCore: true, wantDelta: true},
		{env: "1", classic: true, exeInHost: false, exeInCore: false, wantDelta: false},
		{env: "1", classic: true, exeInHost: false, exeInCore: true, wantDelta: true},
		{env: "1", classic: true, exeInHost: true, exeInCore: false, wantDelta: true},
		{env: "1", classic: true, exeInHost: true, exeInCore: true, wantDelta: true},
	}

	for _, scenario := range scenarios {
		if scenario.exeInCore {
			osutil.CopyFile("/bin/true", exeInCorePath, 0)
		} else {
			os.Remove(exeInCorePath)
		}
		os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", scenario.env)
		release.MockOnClassic(scenario.classic)
		if scenario.exeInHost {
			os.Setenv("PATH", origPath)
		} else {
			os.Setenv("PATH", altPath)
		}

		c.Check(store.UseDeltas(), Equals, scenario.wantDelta, Commentf("%#v", scenario))
	}
}

type downloadBehaviour []struct {
	url   string
	error bool
}

var deltaTests = []struct {
	downloads       downloadBehaviour
	info            snap.DownloadInfo
	expectedContent string
}{{
	// The full snap is not downloaded, but rather the delta
	// is downloaded and applied.
	downloads: downloadBehaviour{
		{url: "delta-url"},
	},
	info: snap.DownloadInfo{
		AnonDownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "delta-url", Format: "xdelta3"},
		},
	},
	expectedContent: "snap-content-via-delta",
}, {
	// If there is an error during the delta download, the
	// full snap is downloaded as per normal.
	downloads: downloadBehaviour{
		{error: true},
		{url: "full-snap-url"},
	},
	info: snap.DownloadInfo{
		AnonDownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "delta-url", Format: "xdelta3"},
		},
	},
	expectedContent: "full-snap-url-content",
}, {
	// If more than one matching delta is returned by the store
	// we ignore deltas and do the full download.
	downloads: downloadBehaviour{
		{url: "full-snap-url"},
	},
	info: snap.DownloadInfo{
		AnonDownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "delta-url", Format: "xdelta3"},
			{AnonDownloadURL: "delta-url-2", Format: "xdelta3"},
		},
	},
	expectedContent: "full-snap-url-content",
}}

func (s *storeTestSuite) TestDownloadWithDelta(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	for _, testCase := range deltaTests {
		testCase.info.Size = int64(len(testCase.expectedContent))
		downloadIndex := 0
		restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
			if testCase.downloads[downloadIndex].error {
				downloadIndex++
				return errors.New("Bang")
			}
			c.Check(url, Equals, testCase.downloads[downloadIndex].url)
			w.Write([]byte(testCase.downloads[downloadIndex].url + "-content"))
			downloadIndex++
			return nil
		})
		defer restore()
		restore = store.MockApplyDelta(func(name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
			c.Check(deltaInfo, Equals, &testCase.info.Deltas[0])
			err := ioutil.WriteFile(targetPath, []byte("snap-content-via-delta"), 0644)
			c.Assert(err, IsNil)
			return nil
		})
		defer restore()

		path := filepath.Join(c.MkDir(), "subdir", "downloaded-file")
		err := s.store.Download(context.TODO(), "foo", path, &testCase.info, nil, nil, nil)

		c.Assert(err, IsNil)
		defer os.Remove(path)
		c.Assert(path, testutil.FileEquals, testCase.expectedContent)
	}
}

var downloadDeltaTests = []struct {
	info          snap.DownloadInfo
	authenticated bool
	deviceSession bool
	useLocalUser  bool
	format        string
	expectedURL   string
	expectError   bool
}{{
	// An unauthenticated request downloads the anonymous delta url.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "anon-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	authenticated: false,
	deviceSession: false,
	format:        "xdelta3",
	expectedURL:   "anon-delta-url",
	expectError:   false,
}, {
	// An authenticated request downloads the authenticated delta url.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "anon-delta-url", DownloadURL: "auth-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	authenticated: true,
	deviceSession: false,
	useLocalUser:  false,
	format:        "xdelta3",
	expectedURL:   "auth-delta-url",
	expectError:   false,
}, {
	// A device-authenticated request downloads the authenticated delta url.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "anon-delta-url", DownloadURL: "auth-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	authenticated: false,
	deviceSession: true,
	useLocalUser:  false,
	format:        "xdelta3",
	expectedURL:   "auth-delta-url",
	expectError:   false,
}, {
	// A local authenticated request downloads the anonymous delta url.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{AnonDownloadURL: "anon-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	authenticated: true,
	deviceSession: false,
	useLocalUser:  true,
	format:        "xdelta3",
	expectedURL:   "anon-delta-url",
	expectError:   false,
}, {
	// An error is returned if more than one matching delta is returned by the store,
	// though this may be handled in the future.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "xdelta3-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 25},
			{DownloadURL: "bsdiff-delta-url", Format: "xdelta3", FromRevision: 25, ToRevision: 26},
		},
	},
	authenticated: false,
	deviceSession: false,
	format:        "xdelta3",
	expectedURL:   "",
	expectError:   true,
}, {
	// If the supported format isn't available, an error is returned.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "xdelta3-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
			{DownloadURL: "ydelta-delta-url", Format: "ydelta", FromRevision: 24, ToRevision: 26},
		},
	},
	authenticated: false,
	deviceSession: false,
	format:        "bsdiff",
	expectedURL:   "",
	expectError:   true,
}}

func (s *storeTestSuite) TestDownloadDelta(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	authContext := &testAuthContext{c: c}
	sto := store.New(nil, authContext)

	for _, testCase := range downloadDeltaTests {
		sto.SetDeltaFormat(testCase.format)
		restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, _ *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
			expectedUser := s.user
			if testCase.useLocalUser {
				expectedUser = s.localUser
			}
			if !testCase.authenticated {
				expectedUser = nil
			}
			c.Check(user, Equals, expectedUser)
			c.Check(url, Equals, testCase.expectedURL)
			w.Write([]byte("I was downloaded"))
			return nil
		})
		defer restore()

		w, err := ioutil.TempFile("", "")
		c.Assert(err, IsNil)
		defer os.Remove(w.Name())

		authContext.device = nil
		if testCase.deviceSession {
			authContext.device = s.device
		}

		authedUser := s.user
		if testCase.useLocalUser {
			authedUser = s.localUser
		}
		if !testCase.authenticated {
			authedUser = nil
		}

		err = sto.DownloadDelta("snapname", &testCase.info, w, nil, authedUser)

		if testCase.expectError {
			c.Assert(err, NotNil)
		} else {
			c.Assert(err, IsNil)
			c.Assert(w.Name(), testutil.FileEquals, "I was downloaded")
		}
	}
}

var applyDeltaTests = []struct {
	deltaInfo       snap.DeltaInfo
	currentRevision uint
	error           string
}{{
	// A supported delta format can be applied.
	deltaInfo:       snap.DeltaInfo{Format: "xdelta3", FromRevision: 24, ToRevision: 26},
	currentRevision: 24,
	error:           "",
}, {
	// An error is returned if the expected current snap does not exist on disk.
	deltaInfo:       snap.DeltaInfo{Format: "xdelta3", FromRevision: 24, ToRevision: 26},
	currentRevision: 23,
	error:           "snap \"foo\" revision 24 not found",
}, {
	// An error is returned if the format is not supported.
	deltaInfo:       snap.DeltaInfo{Format: "nodelta", FromRevision: 24, ToRevision: 26},
	currentRevision: 24,
	error:           "cannot apply unsupported delta format \"nodelta\" (only xdelta3 currently)",
}}

func (s *storeTestSuite) TestApplyDelta(c *C) {
	for _, testCase := range applyDeltaTests {
		name := "foo"
		currentSnapName := fmt.Sprintf("%s_%d.snap", name, testCase.currentRevision)
		currentSnapPath := filepath.Join(dirs.SnapBlobDir, currentSnapName)
		targetSnapName := fmt.Sprintf("%s_%d.snap", name, testCase.deltaInfo.ToRevision)
		targetSnapPath := filepath.Join(dirs.SnapBlobDir, targetSnapName)
		err := os.MkdirAll(filepath.Dir(currentSnapPath), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(currentSnapPath, nil, 0644)
		c.Assert(err, IsNil)
		deltaPath := filepath.Join(dirs.SnapBlobDir, "the.delta")
		err = ioutil.WriteFile(deltaPath, nil, 0644)
		c.Assert(err, IsNil)
		// When testing a case where the call to the external
		// xdelta3 is successful,
		// simulate the resulting .partial.
		if testCase.error == "" {
			err = ioutil.WriteFile(targetSnapPath+".partial", nil, 0644)
			c.Assert(err, IsNil)
		}

		err = store.ApplyDelta(name, deltaPath, &testCase.deltaInfo, targetSnapPath, "")

		if testCase.error == "" {
			c.Assert(err, IsNil)
			c.Assert(s.mockXDelta.Calls(), DeepEquals, [][]string{
				{"xdelta3", "-d", "-s", currentSnapPath, deltaPath, targetSnapPath + ".partial"},
			})
			c.Assert(osutil.FileExists(targetSnapPath+".partial"), Equals, false)
			c.Assert(osutil.FileExists(targetSnapPath), Equals, true)
			c.Assert(os.Remove(targetSnapPath), IsNil)
		} else {
			c.Assert(err, NotNil)
			c.Assert(err.Error()[0:len(testCase.error)], Equals, testCase.error)
			c.Assert(osutil.FileExists(targetSnapPath+".partial"), Equals, false)
			c.Assert(osutil.FileExists(targetSnapPath), Equals, false)
		}
		c.Assert(os.Remove(currentSnapPath), IsNil)
		c.Assert(os.Remove(deltaPath), IsNil)
	}
}

var (
	userAgent = httputil.UserAgent()
)

func (s *storeTestSuite) TestDoRequestSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check user authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))
		// check device authorization is set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
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

	authContext := &testAuthContext{c: c, device: s.device, user: s.localUser}
	sto := store.New(&store.Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.localUser)
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
		c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))
		// check device authorization was not set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// no serial and no device macaroon => no device auth
	s.device.Serial = ""
	s.device.SessionMacaroon = ""
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
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
		c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))
		if s.user.StoreDischarges[0] == refresh {
			io.WriteString(w, "response-data")
		} else {
			w.Header().Set("WWW-Authenticate", "Macaroon needs_refresh=1")
			w.WriteHeader(401)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
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
		c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))
		w.Header().Set("WWW-Authenticate", "Macaroon needs_refresh=1")
		w.WriteHeader(401)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := store.NewRequestOptions("GET", endpoint)

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
	c.Assert(err, Equals, store.ErrInvalidCredentials)
	c.Check(response, IsNil)
	c.Check(refreshDischargeEndpointHit, Equals, true)
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
			// sanity of request
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
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, authContext)

	reqOptions := store.NewRequestOptions("GET", mockServerURL)

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
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
			c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))
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
			// sanity of request
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
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, authContext)

	reqOptions := store.NewRequestOptions("GET", mockServerURL)

	resp, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
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

	response, err := sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
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

	userMacaroon, userDischarge, err := store.LoginUser("username", "password", "otp")

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

	userMacaroon, userDischarge, err := store.LoginUser("username", "password", "otp")

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

	userMacaroon, userDischarge, err := store.LoginUser("username", "password", "otp")

	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: .*")
	c.Check(userMacaroon, Equals, "")
	c.Check(userDischarge, Equals, "")
}

const (
	funkyAppSnapID = "1e21e12ex4iim2xj1g2ul6f12f1"

	helloWorldSnapID      = "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
	helloWorldDeveloperID = "canonical"
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

/* acquired via

http --pretty=format --print b https://api.snapcraft.io/v2/snaps/info/hello-world architecture==amd64 fields==architectures,base,confinement,contact,created-at,description,download,epoch,license,name,prices,private,publisher,revision,snap-id,snap-yaml,summary,title,type,version,media,common-ids Snap-Device-Series:16 | xsel -b

on 2018-06-13 (note snap-yaml is currently excluded from that list). Then, by hand:
- set prices to {"EUR": "0.99", "USD": "1.23"},
- set base in first channel-map entry to "bogus-base",
- set snap-yaml in first channel-map entry to the one from the 'edge', plus the following pastiche:
apps:
  content-plug:
    command: bin/content-plug
    plugs: [shared-content-plug]
plugs:
  shared-content-plug:
    interface: content
    target: import
    content: mylib
    default-provider: test-snapd-content-slot
slots:
  shared-content-slot:
    interface: content
    content: mylib
    read:
      - /

*/
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
                "risk": "stable",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2016-07-12T16:37:23.960632+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 27,
            "snap-yaml": "name: hello-world\nversion: 6.3\narchitectures: [ all ]\nsummary: The 'hello-world' of snaps\ndescription: |\n    This is a simple snap example that includes a few interesting binaries\n    to demonstrate snaps and their confinement.\n    * hello-world.env  - dump the env of commands run inside app sandbox\n    * hello-world.evil - show how snappy sandboxes binaries\n    * hello-world.sh   - enter interactive shell that runs in app sandbox\n    * hello-world      - simply output text\napps:\n env:\n   command: bin/env\n evil:\n   command: bin/evil\n sh:\n   command: bin/sh\n hello-world:\n   command: bin/echo\n content-plug:\n   command: bin/content-plug\n   plugs: [shared-content-plug]\nplugs:\n  shared-content-plug:\n    interface: content\n    target: import\n    content: mylib\n    default-provider: test-snapd-content-slot\nslots:\n  shared-content-slot:\n    interface: content\n    content: mylib\n    read:\n      - /\n",
            "type": "app",
            "version": "6.3"
        },
        {
            "architectures": [
                "all"
            ],
            "base": null,
            "channel": {
                "architecture": "amd64",
                "name": "candidate",
                "risk": "candidate",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2016-07-12T16:37:23.960632+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 27,
            "snap-yaml": "",
            "type": "app",
            "version": "6.3"
        },
        {
            "architectures": [
                "all"
            ],
            "base": null,
            "channel": {
                "architecture": "amd64",
                "name": "beta",
                "risk": "beta",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2016-07-12T16:37:23.960632+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 27,
            "snap-yaml": "",
            "type": "app",
            "version": "6.3"
        },
        {
            "architectures": [
                "all"
            ],
            "base": null,
            "channel": {
                "architecture": "amd64",
                "name": "edge",
                "risk": "edge",
                "track": "latest"
            },
            "common-ids": [],
            "confinement": "strict",
            "created-at": "2017-11-20T07:59:46.563940+00:00",
            "download": {
                "deltas": [],
                "sha3-384": "d888ed75a9071ace39fed922aa799cad4081de79fda650fbbf75e1bae780dae2c24a19aab8db5059c6ad0d0533d90c04",
                "size": 20480,
                "url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_28.snap"
            },
            "epoch": {
                "read": [
                    0
                ],
                "write": [
                    0
                ]
            },
            "revision": 28,
            "snap-yaml": "",
            "type": "app",
            "version": "6.3"
        }
    ],
    "name": "hello-world",
    "snap": {
        "contact": "mailto:snappy-devel@lists.ubuntu.com",
        "description": "This is a simple hello world example.",
        "license": "MIT",
        "media": [
            {
                "height": null,
                "type": "icon",
                "url": "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "width": null
            },
            {
                "height": null,
                "type": "screenshot",
                "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png",
                "width": null
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
        "title": "Hello World"
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
		c.Check(query.Get("architecture"), Equals, arch.UbuntuArchitecture())

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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.InstanceName(), Equals, "hello-world")
	c.Check(result.Architectures, DeepEquals, []string{"all"})
	c.Check(result.Revision, Equals, snap.R(27))
	c.Check(result.SnapID, Equals, helloWorldSnapID)
	c.Check(result.Publisher, Equals, snap.StoreAccount{
		ID:          "canonical",
		Username:    "canonical",
		DisplayName: "Canonical",
		Validation:  "verified",
	})
	c.Check(result.Version, Equals, "6.3")
	c.Check(result.Sha3_384, Matches, `[[:xdigit:]]{96}`)
	c.Check(result.Size, Equals, int64(20480))
	c.Check(result.Channel, Equals, "stable")
	c.Check(result.Description(), Equals, "This is a simple hello world example.")
	c.Check(result.Summary(), Equals, "The 'hello-world' of snaps")
	c.Check(result.Title(), Equals, "Hello World") // TODO: have this updated to be different to the name
	c.Check(result.License, Equals, "MIT")
	c.Check(result.Prices, DeepEquals, map[string]float64{"EUR": 0.99, "USD": 1.23})
	c.Check(result.Paid, Equals, true)
	c.Check(result.Screenshots, DeepEquals, []snap.ScreenshotInfo{
		{
			URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png",
		},
	})
	c.Check(result.MustBuy, Equals, true)
	c.Check(result.Contact, Equals, "mailto:snappy-devel@lists.ubuntu.com")
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	info, err := sto.SnapInfo(store.SnapSpec{Name: "hello"}, nil)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "hello")

	info, err = sto.SnapInfo(store.SnapSpec{Name: "hello"}, nil)
	c.Check(err, Equals, store.ErrSnapNotFound)
	c.Check(info, IsNil)

	info, err = sto.SnapInfo(store.SnapSpec{Name: "hello"}, nil)
	c.Check(err, Equals, store.ErrSnapNotFound)
	c.Check(info, IsNil)

	info, err = sto.SnapInfo(store.SnapSpec{Name: "hello"}, nil)
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	_, err := sto.SnapInfo(spec, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world": got unexpected HTTP status code 500 via GET to "http://.*?/info/hello-world.*"`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestInfo500once(c *C) {
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Check(result.InstanceName(), Equals, "hello-world")
	expected := map[string]*snap.ChannelSnapInfo{
		"latest/stable": {
			Revision:    snap.R(27),
			Version:     "6.3",
			Confinement: snap.StrictConfinement,
			Channel:     "stable",
			Size:        20480,
			Epoch:       *snap.E("0"),
		},
		"latest/candidate": {
			Revision:    snap.R(27),
			Version:     "6.3",
			Confinement: snap.StrictConfinement,
			Channel:     "candidate",
			Size:        20480,
			Epoch:       *snap.E("0"),
		},
		"latest/beta": {
			Revision:    snap.R(27),
			Version:     "6.3",
			Confinement: snap.StrictConfinement,
			Channel:     "beta",
			Size:        20480,
			Epoch:       *snap.E("0"),
		},
		"latest/edge": {
			Revision:    snap.R(28),
			Version:     "6.3",
			Confinement: snap.StrictConfinement,
			Channel:     "edge",
			Size:        20480,
			Epoch:       *snap.E("0"),
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
		// following is just a tweaked version of:
		// http https://api.snapcraft.io/v2/snaps/info/go architecture==amd64 fields==channel Snap-Device-Series:16 | jq -c '.["channel-map"] | .[]'
		io.WriteString(w, `{"channel-map": [
{"channel":{"name":"stable",      "risk":"stable", "track":"latest"}},
{"channel":{"name":"edge",        "risk":"edge",   "track":"latest"}},
{"channel":{"name":"1.10/stable", "risk":"stable", "track":"1.10"  }},
{"channel":{"name":"1.6/stable",  "risk":"stable", "track":"1.6"   }},
{"channel":{"name":"1.7/stable",  "risk":"stable", "track":"1.7"   }},
{"channel":{"name":"1.8/stable",  "risk":"stable", "track":"1.8"   }},
{"channel":{"name":"1.9/stable",  "risk":"stable", "track":"1.9"   }}
]}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	// the actual test
	result, err := sto.SnapInfo(store.SnapSpec{Name: "eh"}, nil)
	c.Assert(err, IsNil)
	expected := map[string]*snap.ChannelSnapInfo{
		"latest/stable": {Channel: "stable"},
		"latest/edge":   {Channel: "edge"},
		"1.6/stable":    {Channel: "1.6/stable"},
		"1.7/stable":    {Channel: "1.7/stable"},
		"1.8/stable":    {Channel: "1.8/stable"},
		"1.9/stable":    {Channel: "1.9/stable"},
		"1.10/stable":   {Channel: "1.10/stable"},
	}
	for k, v := range result.Channels {
		c.Check(v, DeepEquals, expected[k], Commentf("%q", k))
	}
	c.Check(result.Channels, HasLen, len(expected))
	c.Check(result.Tracks, DeepEquals, []string{"latest", "1.10", "1.6", "1.7", "1.8", "1.9"})
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
	result, err := sto.SnapInfo(spec, nil)
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
	sto := store.New(cfg, &testAuthContext{c: c, device: s.device, storeID: "my-brand-store-id"})

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
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
	sto := store.New(cfg, &testAuthContext{
		c:             c,
		device:        s.device,
		proxyStoreID:  "foo",
		proxyStoreURL: mockServerURL,
	})

	// the actual test
	spec := store.SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
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
	sto := store.New(cfg, &testAuthContext{
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
	result, err := sto.SnapInfo(spec, nil)
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
	_, err := sto.SnapInfo(spec, nil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world": got unexpected HTTP status code 5.. via GET to "http://\S+" \[OOPS-[[:xdigit:]]*\]`)
}

/*
acquired via

http --pretty=format --print b https://api.snapcraft.io/v2/snaps/info/no:such:package architecture==amd64 fields==architectures,base,confinement,contact,created-at,description,download,epoch,license,name,prices,private,publisher,revision,snap-id,snap-yaml,summary,title,type,version,media,common-ids Snap-Device-Series:16 | xsel -b

on 2018-06-14

*/
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
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, NotNil)
	c.Assert(result, IsNil)
}

/* acquired via:
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64" 'https://api.snapcraft.io/api/v1/snaps/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cepoch%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Clicense%2Cbase%2Csupport_url%2Ccontact%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cdeveloper_name%2Cdeveloper_validation%2Cprivate%2Cconfinement%2Ccommon_ids&q=hello' | python -m json.tool | xsel -b
Add base and prices.
*/
const MockSearchJSON = `{
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
                "contact": "mailto:snappy-devel@lists.ubuntu.com",
                "content": "application",
                "description": "This is a simple hello world example.",
                "developer_id": "canonical",
                "developer_name": "Canonical",
                "developer_validation": "verified",
                "download_sha3_384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
                "download_url": "https://api.snapcraft.io/api/v1/snaps/download/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
                "epoch": "0",
                "icon_url": "https://dashboard.snapcraft.io/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2016-07-12T16:37:23.960632+00:00",
                "license": "MIT",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {"EUR": 2.99, "USD": 3.49},
                "private": false,
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 27,
                "screenshot_urls": [
                    "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png"
                ],
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "The 'hello-world' of snaps",
                "support_url": "",
                "title": "Hello World",
                "version": "6.3"
            }
        ]
    },
    "_links": {
        "self": {
            "href": "http://api.snapcraft.io/api/v1/snaps/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cepoch%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Clicense%2Cbase%2Csupport_url%2Ccontact%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement%2Ccommon_ids&q=hello"
        }
    }
}
`

func (s *storeTestSuite) TestFindQueries(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")
		section := query.Get("section")

		c.Check(r.URL.Path, Matches, ".*/search")
		c.Check(query.Get("fields"), Equals, "abc,def")

		// write dummy json so that Find doesn't re-try due to json decoder EOF error
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
			c.Check(query.Get("scope"), Equals, "maastricht")
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	for _, query := range []store.Search{
		{Query: "hello", Prefix: true},
		{Query: "hello", Scope: "maastricht"},
		{Section: "db"},
		{Query: "hello", Section: "db"},
	} {
		sto.Find(&query, nil)
	}
}

/* acquired via:
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	sections, err := sto.Sections(context.TODO(), s.user)
	c.Check(err, IsNil)
	c.Check(sections, DeepEquals, []string{"featured", "database"})
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
	authContext := &testAuthContext{c: c, device: s.device, storeID: "my-brand-store"}
	sto := store.New(&cfg, authContext)

	sections, err := sto.Sections(context.TODO(), s.user)
	c.Check(err, IsNil)
	c.Check(sections, DeepEquals, []string{"featured", "database"})
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
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&store.Config{StoreBaseURL: serverURL}, authContext)

	db, err := advisor.Create()
	c.Assert(err, IsNil)
	defer db.Rollback()

	var bufNames bytes.Buffer
	err = sto.WriteCatalogs(context.TODO(), &bufNames, db)
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
}

func (s *storeTestSuite) TestFind(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		query := r.URL.Query()

		q := query.Get("q")
		c.Check(q, Equals, "hello")

		c.Check(r.UserAgent(), Equals, userAgent)

		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Query().Get("fields"), Equals, "abc,def")

		c.Check(r.Header.Get("X-Ubuntu-Series"), Equals, release.Series)
		c.Check(r.Header.Get("X-Ubuntu-Architecture"), Equals, arch.UbuntuArchitecture())
		c.Check(r.Header.Get("X-Ubuntu-Classic"), Equals, "false")

		c.Check(r.Header.Get("X-Ubuntu-Confinement"), Equals, "")

		w.Header().Set("X-Suggested-Currency", "GBP")

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(200)

		io.WriteString(w, MockSearchJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	snaps, err := sto.Find(&store.Search{Query: "hello"}, nil)
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	snp := snaps[0]
	c.Check(snp.InstanceName(), Equals, "hello-world")
	c.Check(snp.Architectures, DeepEquals, []string{"all"})
	c.Check(snp.Revision, Equals, snap.R(27))
	c.Check(snp.SnapID, Equals, helloWorldSnapID)
	c.Check(snp.Publisher, Equals, snap.StoreAccount{
		ID:          "canonical",
		Username:    "canonical",
		DisplayName: "Canonical",
		Validation:  "verified",
	})
	c.Check(snp.Version, Equals, "6.3")
	c.Check(snp.Sha3_384, Matches, `[[:xdigit:]]{96}`)
	c.Check(snp.Size, Equals, int64(20480))
	c.Check(snp.Channel, Equals, "stable")
	c.Check(snp.Description(), Equals, "This is a simple hello world example.")
	c.Check(snp.Summary(), Equals, "The 'hello-world' of snaps")
	c.Check(snp.Title(), Equals, "Hello World")
	c.Check(snp.License, Equals, "MIT")
	c.Assert(snp.Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Assert(snp.Paid, Equals, true)
	c.Assert(snp.Screenshots, DeepEquals, []snap.ScreenshotInfo{
		{
			URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/06/Screenshot_from_2018-06-14_09-33-31.png",
		},
	})
	c.Check(snp.MustBuy, Equals, true)
	c.Check(snp.Contact, Equals, "mailto:snappy-devel@lists.ubuntu.com")
	c.Check(snp.Base, Equals, "bare-base")

	// Make sure the epoch (currently not sent by the store) defaults to "0"
	c.Check(snp.Epoch.String(), Equals, "0")

	c.Check(sto.SuggestedCurrency(), Equals, "GBP")
}

func (s *storeTestSuite) TestFindPrivate(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")

		switch n {
		case 0:
			c.Check(r.URL.Path, Matches, ".*/search")
			c.Check(name, Equals, "")
			c.Check(q, Equals, "foo")
			c.Check(query.Get("private"), Equals, "true")
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(200)
		io.WriteString(w, strings.Replace(MockSearchJSON, `"EUR": 2.99, "USD": 3.49`, "", -1))

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Find(&store.Search{Query: "foo", Private: true}, s.user)
	c.Check(err, IsNil)

	_, err = sto.Find(&store.Search{Query: "foo", Private: true}, nil)
	c.Check(err, Equals, store.ErrUnauthenticated)

	_, err = sto.Find(&store.Search{Query: "name:foo", Private: true}, s.user)
	c.Check(err, Equals, store.ErrBadQuery)
}

func (s *storeTestSuite) TestFindFailures(c *C) {
	sto := store.New(&store.Config{StoreBaseURL: new(url.URL)}, nil)
	_, err := sto.Find(&store.Search{Query: "foo:bar"}, nil)
	c.Check(err, Equals, store.ErrBadQuery)
	_, err = sto.Find(&store.Search{Query: "foo", Private: true, Prefix: true}, s.user)
	c.Check(err, Equals, store.ErrBadQuery)
}

func (s *storeTestSuite) TestFindFails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		http.Error(w, http.StatusText(418), 418) // I'm a teapot
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(&store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `cannot search: got unexpected HTTP status code 418 via GET to "http://\S+[?&]q=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (s *storeTestSuite) TestFindBadContentType(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(&store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `received an unexpected content type \("text/plain[^"]+"\) when trying to search via "http://\S+[?&]q=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (s *storeTestSuite) TestFindBadBody(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		query := r.URL.Query()
		c.Check(query.Get("q"), Equals, "hello")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, "<hello>")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(&store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `invalid character '<' looking for beginning of value`)
	c.Check(snaps, HasLen, 0)
}

func (s *storeTestSuite) TestFind500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
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
	sto := store.New(&cfg, nil)

	_, err := sto.Find(&store.Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `cannot search: got unexpected HTTP status code 500 via GET to "http://\S+[?&]q=hello.*"`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestFind500once(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		n++
		if n == 1 {
			w.WriteHeader(500)
		} else {
			w.Header().Set("Content-Type", "application/hal+json")
			w.WriteHeader(200)
			io.WriteString(w, strings.Replace(MockSearchJSON, `"EUR": 2.99, "USD": 3.49`, "", -1))
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
	}
	sto := store.New(&cfg, nil)

	snaps, err := sto.Find(&store.Search{Query: "hello"}, nil)
	c.Check(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestFindAuthFailed(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case searchPath:
			// check authorization is set
			authorization := r.Header.Get("Authorization")
			c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))

			query := r.URL.Query()
			c.Check(query.Get("q"), Equals, "foo")
			if release.OnClassic {
				c.Check(query.Get("confinement"), Matches, `strict,classic|classic,strict`)
			} else {
				c.Check(query.Get("confinement"), Equals, "strict")
			}
			w.Header().Set("Content-Type", "application/hal+json")
			io.WriteString(w, MockSearchJSON)
		case ordersPath:
			c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
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

	snaps, err := sto.Find(&store.Search{Query: "foo"}, s.user)
	c.Assert(err, IsNil)

	// Check that we log an error.
	c.Check(s.logbuf.String(), Matches, "(?ms).* cannot get user orders: invalid credentials")

	// But still successfully return snap information.
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].SnapID, Equals, helloWorldSnapID)
	c.Check(snaps[0].Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Check(snaps[0].MustBuy, Equals, true)
}

func (s *storeTestSuite) TestFindCommonIDs(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", searchPath)
		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")

		switch n {
		case 0:
			c.Check(r.URL.Path, Matches, ".*/search")
			c.Check(name, Equals, "")
			c.Check(q, Equals, "foo")
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(200)
		io.WriteString(w, strings.Replace(MockSearchJSON,
			`"common_ids": []`,
			`"common_ids": ["org.hello"]`, -1))

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: serverURL,
	}
	sto := store.New(&cfg, nil)

	infos, err := sto.Find(&store.Search{Query: "foo"}, nil)
	c.Check(err, IsNil)
	c.Assert(infos, HasLen, 1)
	c.Check(infos[0].CommonIDs, DeepEquals, []string{"org.hello"})
}

func (s *storeTestSuite) TestCurrentSnap(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(1),
		Epoch:    *snap.E("1"),
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Channel, Equals, cand.Channel)
	c.Check(cs.Epoch, DeepEquals, cand.Epoch)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(cs.IgnoreValidation, Equals, cand.IgnoreValidation)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapIgnoreValidation(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:           helloWorldSnapID,
		Channel:          "stable",
		Revision:         snap.R(1),
		Epoch:            *snap.E("1"),
		IgnoreValidation: true,
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Channel, Equals, cand.Channel)
	c.Check(cs.Epoch, DeepEquals, cand.Epoch)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(cs.IgnoreValidation, Equals, cand.IgnoreValidation)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapNoChannel(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Revision: snap.R(1),
		Epoch:    *snap.E("1"),
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Channel, Equals, "stable")
	c.Check(cs.Epoch, DeepEquals, cand.Epoch)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapNilNoID(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:   "",
		Revision: snap.R(1),
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, IsNil)
	c.Check(s.logbuf.String(), Matches, "(?m).* an empty SnapID but a store revision!")
}

func (s *storeTestSuite) TestCurrentSnapNilLocalRevision(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Revision: snap.R("x1"),
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, IsNil)
	c.Check(s.logbuf.String(), Matches, "(?m).* a non-empty SnapID but a non-store revision!")
}

func (s *storeTestSuite) TestCurrentSnapNilLocalRevisionNoID(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:   "",
		Revision: snap.R("x1"),
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, IsNil)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapRevLocalRevWithAmendHappy(c *C) {
	cand := &store.RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Revision: snap.R("x1"),
		Amend:    true,
	}
	cs := store.GetCurrentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestAuthLocationDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := store.AuthLocation()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := store.AuthLocation()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestAuthURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := store.AuthURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := store.AuthURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestApiURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := store.ApiURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
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
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://force-api.local/: missing protocol scheme")
}

func (s *storeTestSuite) TestStoreURLBadEnvironCPI(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", "://force-cpi.local/api/v1/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_CPI_URL", "")
	_, err := store.StoreURL(store.ApiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_CPI_URL: parse ://force-cpi.local/: missing protocol scheme")
}

func (s *storeTestSuite) TestStoreDeveloperURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := store.StoreDeveloperURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := store.StoreDeveloperURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TeststoreDefaultConfig(c *C) {
	c.Check(store.DefaultConfig().StoreBaseURL.String(), Equals, "https://api.snapcraft.io/")
	c.Check(store.DefaultConfig().AssertionsBaseURL, IsNil)
}

func (s *storeTestSuite) TestNew(c *C) {
	aStore := store.New(nil, nil)
	c.Assert(aStore, NotNil)
	// check for fields
	c.Check(aStore.DetailFields(), DeepEquals, store.DefaultConfig().DetailFields)
}

var testAssertion = `type: snap-declaration
authority-id: super
series: 16
snap-id: snapidfoo
publisher-id: devidbaz
snap-name: mysnap
timestamp: 2016-03-30T12:22:16Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

openpgp wsBcBAABCAAQBQJW+8VBCRDWhXkqAWcrfgAAQ9gIABZFgMPByJZeUE835FkX3/y2hORn
AzE3R1ktDkQEVe/nfVDMACAuaw1fKmUS4zQ7LIrx/AZYw5i0vKVmJszL42LBWVsqR0+p9Cxebzv9
U2VUSIajEsUUKkBwzD8wxFzagepFlScif1NvCGZx0vcGUOu0Ent0v+gqgAv21of4efKqEW7crlI1
T/A8LqZYmIzKRHGwCVucCyAUD8xnwt9nyWLgLB+LLPOVFNK8SR6YyNsX05Yz1BUSndBfaTN8j/k8
8isKGZE6P0O9ozBbNIAE8v8NMWQegJ4uWuil7D3psLkzQIrxSypk9TrQ2GlIG2hJdUovc5zBuroe
xS4u9rVT6UY=`

func (s *storeTestSuite) TestAssertion(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	a, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (s *storeTestSuite) TestAssertionProxyStoreFromAuthContext(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	nowhereURL, err := url.Parse("http://nowhere.invalid")
	c.Assert(err, IsNil)
	cfg := store.Config{
		AssertionsBaseURL: nowhereURL,
	}
	authContext := &testAuthContext{
		c:             c,
		device:        s.device,
		proxyStoreID:  "foo",
		proxyStoreURL: mockServerURL,
	}
	sto := store.New(&cfg, authContext)

	a, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (s *storeTestSuite) TestAssertionNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		io.WriteString(w, `{"status": 404,"title": "not found"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Check(asserts.IsNotFound(err), Equals, true)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.SnapDeclarationType,
		Headers: map[string]string{
			"series":  "16",
			"snap-id": "snapidfoo",
		},
	})
}

func (s *storeTestSuite) TestAssertion500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, ErrorMatches, `cannot fetch assertion: got unexpected HTTP status code 500 via .+`)
	c.Assert(n, Equals, 5)
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
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(sto.SuggestedCurrency(), Equals, "GBP")

	suggestedCurrency = "EUR"

	// checking the currency updates
	result, err = sto.SnapInfo(spec, nil)
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
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockOrdersJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, authContext)

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
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
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
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockSingleOrderJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, authContext)

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
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(404)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, authContext)

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
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, store.JsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(401)
		io.WriteString(w, "")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, authContext)

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
	expectedResult    *store.BuyResult
	expectedError     string
}{
	{
		// successful buying
		suggestedCurrency: "EUR",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"0.99","currency":"EUR"}`,
		buyResponse:       mockOrderResponseJSON,
		expectedResult:    &store.BuyResult{State: "Complete"},
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
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, authContext)

	buyOptions := &store.BuyOptions{
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
				c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
				io.WriteString(w, `{"orders": []}`)
				purchaseServerGetCalled = true
			case buyPath:
				c.Assert(r.Method, Equals, "POST")
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
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
		authContext := &testAuthContext{c: c, device: s.device, user: s.user}
		cfg := store.Config{
			StoreBaseURL: mockServerURL,
		}
		sto := store.New(&cfg, authContext)

		// Find the snap first
		spec := store.SnapSpec{
			Name: "hello-world",
		}
		snap, err := sto.SnapInfo(spec, s.user)
		c.Assert(snap, NotNil)
		c.Assert(err, IsNil)

		buyOptions := &store.BuyOptions{
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
	result, err := sto.Buy(&store.BuyOptions{
		Price:    1.0,
		Currency: "USD",
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: snap ID missing")

	// no price
	result, err = sto.Buy(&store.BuyOptions{
		SnapID:   "snap ID",
		Currency: "USD",
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: invalid expected price")

	// no currency
	result, err = sto.Buy(&store.BuyOptions{
		SnapID: "snap ID",
		Price:  1.0,
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: currency missing")

	// no user
	result, err = sto.Buy(&store.BuyOptions{
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
				c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
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
		authContext := &testAuthContext{c: c, device: s.device, user: s.user}
		cfg := store.Config{
			StoreBaseURL: mockServerURL,
		}
		sto := store.New(&cfg, authContext)

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
	_, err = sto.DoRequest(context.TODO(), sto.Client(), reqOptions, s.user)
	c.Assert(err, IsNil)
}

type cacheObserver struct {
	inCache map[string]bool

	gets []string
	puts []string
}

func (co *cacheObserver) Get(cacheKey, targetPath string) error {
	co.gets = append(co.gets, fmt.Sprintf("%s:%s", cacheKey, targetPath))
	if !co.inCache[cacheKey] {
		return fmt.Errorf("cannot find %s in cache", cacheKey)
	}
	return nil
}
func (co *cacheObserver) Put(cacheKey, sourcePath string) error {
	co.puts = append(co.puts, fmt.Sprintf("%s:%s", cacheKey, sourcePath))
	return nil
}

func (s *storeTestSuite) TestDownloadCacheHit(c *C) {
	obs := &cacheObserver{inCache: map[string]bool{"the-snaps-sha3_384": true}}
	restore := s.store.MockCacher(obs)
	defer restore()

	restore = store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		c.Fatalf("download should not be called when results come from the cache")
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.Sha3_384 = "the-snaps-sha3_384"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)

	c.Check(obs.gets, DeepEquals, []string{fmt.Sprintf("%s:%s", snap.Sha3_384, path)})
	c.Check(obs.puts, IsNil)
}

func (s *storeTestSuite) TestDownloadCacheMiss(c *C) {
	obs := &cacheObserver{inCache: map[string]bool{}}
	restore := s.store.MockCacher(obs)
	defer restore()

	downloadWasCalled := false
	restore = store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		downloadWasCalled = true
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.Sha3_384 = "the-snaps-sha3_384"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Check(downloadWasCalled, Equals, true)

	c.Check(obs.gets, DeepEquals, []string{fmt.Sprintf("the-snaps-sha3_384:%s", path)})
	c.Check(obs.puts, DeepEquals, []string{fmt.Sprintf("the-snaps-sha3_384:%s", path)})
}

var (
	helloRefreshedDateStr = "2018-02-27T11:00:00Z"
	helloRefreshedDate    time.Time
)

func init() {
	t, err := time.Parse(time.RFC3339, helloRefreshedDateStr)
	if err != nil {
		panic(err)
	}
	helloRefreshedDate = t
}

func (s *storeTestSuite) TestSnapAction(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.UbuntuArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Fields  []string                 `json:"fields"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Check(req.Fields, DeepEquals, store.SnapActionFields)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestSnapActionNoResults(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 0)
		io.WriteString(w, `{
  "results": []
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, nil, nil, nil)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})

	// local no-op
	results, err = sto.SnapAction(context.TODO(), nil, nil, nil, nil)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})

	c.Check(err.Error(), Equals, "no install/refresh information results from the store")
}

func (s *storeTestSuite) TestSnapActionRefreshedDateIsOptional(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":      helloWorldSnapID,
			"instance-key": helloWorldSnapID,

			"revision":         float64(1),
			"tracking-channel": "beta",
		})
		c.Assert(req.Actions, HasLen, 0)
		io.WriteString(w, `{
  "results": []
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
		},
	}, nil, nil, nil)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})
}

func (s *storeTestSuite) TestSnapActionSkipBlocked(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
			Block:           []snap.Revision{snap.R(26)},
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": store.ErrNoUpdateAvailable,
		},
	})
}

func (s *storeTestSuite) TestSnapActionSkipCurrent(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": store.ErrNoUpdateAvailable,
		},
	})
}

func (s *storeTestSuite) TestSnapActionRetryOnEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		n++
		if n < 4 {
			io.WriteString(w, "{")
			mockServer.CloseClientConnections()
			return
		}

		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		c.Assert(err, IsNil)
		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Actions, HasLen, 1)
		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
}

func (s *storeTestSuite) TestSnapActionIgnoreValidation(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":           helloWorldSnapID,
			"instance-key":      helloWorldSnapID,
			"revision":          float64(1),
			"tracking-channel":  "stable",
			"refreshed-date":    helloRefreshedDateStr,
			"ignore-validation": true,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":            "refresh",
			"instance-key":      helloWorldSnapID,
			"snap-id":           helloWorldSnapID,
			"channel":           "stable",
			"ignore-validation": false,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:     "hello-world",
			SnapID:           helloWorldSnapID,
			TrackingChannel:  "stable",
			Revision:         snap.R(1),
			RefreshedDate:    helloRefreshedDate,
			IgnoreValidation: true,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
			Flags:        store.SnapActionEnforceValidation,
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeTestSuite) TestInstallFallbackChannelIsStable(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:  "hello-world",
			SnapID:        helloWorldSnapID,
			RefreshedDate: helloRefreshedDate,
			Revision:      snap.R(1),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
}

func (s *storeTestSuite) TestSnapActionNonDefaultsHeaders(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "foo")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, "21")
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, "archXYZ")
		c.Check(r.Header.Get("Snap-Classic"), Equals, "true")

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "foo"
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			RefreshedDate:   helloRefreshedDate,
			Revision:        snap.R(1),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestSnapActionWithDeltas(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Accept-Delta-Format"), Equals, "xdelta3")
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeTestSuite) TestSnapActionOptions(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "true")

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, &store.RefreshOptions{RefreshManaged: true})
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeTestSuite) TestSnapActionInstall(c *C) {
	s.testSnapActionGet("install", c)
}
func (s *storeTestSuite) TestSnapActionDownload(c *C) {
	s.testSnapActionGet("download", c)
}
func (s *storeTestSuite) testSnapActionGet(action string, c *C) {
	// action here is one of install or download
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.UbuntuArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       action,
			"instance-key": action + "-1",
			"name":         "hello-world",
			"channel":      "beta",
		})

		fmt.Fprintf(w, `{
  "results": [{
     "result": "%s",
     "instance-key": "%[1]s-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "effective-channel": "candidate",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`, action)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), nil,
		[]*store.SnapAction{
			{
				Action:       action,
				InstanceName: "hello-world",
				Channel:      "beta",
			},
		}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel
	c.Assert(results[0].Channel, Equals, "candidate")
}
func (s *storeTestSuite) TestSnapActionDownloadParallelInstanceKey(c *C) {
	// action here is one of install or download
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("should not be reached")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	_, err := sto.SnapAction(context.TODO(), nil,
		[]*store.SnapAction{
			{
				Action:       "download",
				InstanceName: "hello-world_foo",
				Channel:      "beta",
			},
		}, nil, nil)
	c.Assert(err, ErrorMatches, `internal error: unsupported download with instance name "hello-world_foo"`)
}

func (s *storeTestSuite) TestSnapActionInstallWithRevision(c *C) {
	s.testSnapActionGetWithRevision("install", c)
}

func (s *storeTestSuite) TestSnapActionDownloadWithRevision(c *C) {
	s.testSnapActionGetWithRevision("download", c)
}

func (s *storeTestSuite) testSnapActionGetWithRevision(action string, c *C) {
	// action here is one of install or download
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.UbuntuArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       action,
			"instance-key": action + "-1",
			"name":         "hello-world",
			"revision":     float64(28),
		})

		fmt.Fprintf(w, `{
  "results": [{
     "result": "%s",
     "instance-key": "%[1]s-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`, action)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), nil,
		[]*store.SnapAction{
			{
				Action:       action,
				InstanceName: "hello-world",
				Revision:     snap.R(28),
			},
		}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(28))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel is not set
	c.Assert(results[0].Channel, Equals, "")
}

func (s *storeTestSuite) TestSnapActionRevisionNotAvailable(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          "snap2-id",
			"instance-key":     "snap2-id",
			"revision":         float64(2),
			"tracking-channel": "edge",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 4)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})
		c.Assert(req.Actions[1], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": "snap2-id",
			"snap-id":      "snap2-id",
			"channel":      "candidate",
		})
		c.Assert(req.Actions[2], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo",
			"channel":      "stable",
		})
		c.Assert(req.Actions[3], DeepEquals, map[string]interface{}{
			"action":       "download",
			"instance-key": "download-1",
			"name":         "bar",
			"revision":     42.,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "error": {
       "code": "revision-not-found",
       "message": "msg1"
     }
  }, {
     "result": "error",
     "instance-key": "snap2-id",
     "snap-id": "snap2-id",
     "name": "snap2",
     "error": {
       "code": "revision-not-found",
       "message": "msg1",
       "extra": {
         "releases": [{"architecture": "amd64", "channel": "beta"},
                      {"architecture": "arm64", "channel": "beta"}]
       }
     }
  }, {
     "result": "error",
     "instance-key": "install-1",
     "snap-id": "foo-id",
     "name": "foo",
     "error": {
       "code": "revision-not-found",
       "message": "msg2"
     }
  }, {
     "result": "error",
     "instance-key": "download-1",
     "snap-id": "bar-id",
     "name": "bar",
     "error": {
       "code": "revision-not-found",
       "message": "msg3"
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
		{
			InstanceName:    "snap2",
			SnapID:          "snap2-id",
			TrackingChannel: "edge",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			InstanceName: "hello-world",
			SnapID:       helloWorldSnapID,
		}, {
			Action:       "refresh",
			InstanceName: "snap2",
			SnapID:       "snap2-id",
			Channel:      "candidate",
		}, {
			Action:       "install",
			InstanceName: "foo",
			Channel:      "stable",
		}, {
			Action:       "download",
			InstanceName: "bar",
			Revision:     snap.R(42),
		},
	}, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "stable",
			},
			"snap2": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "candidate",
				Releases: []snap.Channel{
					snaptest.MustParseChannel("beta", "amd64"),
					snaptest.MustParseChannel("beta", "arm64"),
				},
			},
		},
		Install: map[string]error{
			"foo": &store.RevisionNotAvailableError{
				Action:  "install",
				Channel: "stable",
			},
		},
		Download: map[string]error{
			"bar": &store.RevisionNotAvailableError{
				Action:  "download",
				Channel: "",
			},
		},
	})
}

func (s *storeTestSuite) TestSnapActionSnapNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 3)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})
		c.Assert(req.Actions[1], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo",
			"channel":      "stable",
		})
		c.Assert(req.Actions[2], DeepEquals, map[string]interface{}{
			"action":       "download",
			"instance-key": "download-1",
			"name":         "bar",
			"revision":     42.,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "error": {
       "code": "id-not-found",
       "message": "msg1"
     }
  }, {
     "result": "error",
     "instance-key": "install-1",
     "name": "foo",
     "error": {
       "code": "name-not-found",
       "message": "msg2"
     }
  }, {
     "result": "error",
     "instance-key": "download-1",
     "name": "bar",
     "error": {
       "code": "name-not-found",
       "message": "msg3"
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		}, {
			Action:       "install",
			InstanceName: "foo",
			Channel:      "stable",
		}, {
			Action:       "download",
			InstanceName: "bar",
			Revision:     snap.R(42),
		},
	}, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": store.ErrSnapNotFound,
		},
		Install: map[string]error{
			"foo": store.ErrSnapNotFound,
		},
		Download: map[string]error{
			"bar": store.ErrSnapNotFound,
		},
	})
}

func (s *storeTestSuite) TestSnapActionOtherErrors(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo",
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "error": {
       "code": "other1",
       "message": "other error one"
     }
  }],
  "error-list": [
     {"code": "global-error", "message": "global error"}
  ]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), nil, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "foo",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Other: []error{
			fmt.Errorf("other error one"),
			fmt.Errorf("global error"),
		},
	})
}

func (s *storeTestSuite) TestSnapActionUnknownAction(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("should not have made it to the server")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), nil,
		[]*store.SnapAction{
			{
				Action:       "something unexpected",
				InstanceName: "hello-world",
			},
		}, nil, nil)
	c.Assert(err, ErrorMatches, `.* unsupported action .*`)
	c.Assert(results, IsNil)
}

func (s *storeTestSuite) TestSnapActionErrorError(c *C) {
	e := &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh"),
	}}
	c.Check(e.Error(), Equals, `cannot refresh snap "foo": sad refresh`)

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
		"bar": fmt.Errorf("sad refresh 2"),
	}}
	errMsg := e.Error()
	c.Check(strings.HasPrefix(errMsg, "cannot refresh:"), Equals, true)
	c.Check(errMsg, testutil.Contains, "\nsad refresh 1: \"foo\"")
	c.Check(errMsg, testutil.Contains, "\nsad refresh 2: \"bar\"")

	e = &store.SnapActionError{Install: map[string]error{
		"foo": fmt.Errorf("sad install"),
	}}
	c.Check(e.Error(), Equals, `cannot install snap "foo": sad install`)

	e = &store.SnapActionError{Install: map[string]error{
		"foo": fmt.Errorf("sad install 1"),
		"bar": fmt.Errorf("sad install 2"),
	}}
	errMsg = e.Error()
	c.Check(strings.HasPrefix(errMsg, "cannot install:\n"), Equals, true)
	c.Check(errMsg, testutil.Contains, "\nsad install 1: \"foo\"")
	c.Check(errMsg, testutil.Contains, "\nsad install 2: \"bar\"")

	e = &store.SnapActionError{Download: map[string]error{
		"foo": fmt.Errorf("sad download"),
	}}
	c.Check(e.Error(), Equals, `cannot download snap "foo": sad download`)

	e = &store.SnapActionError{Download: map[string]error{
		"foo": fmt.Errorf("sad download 1"),
		"bar": fmt.Errorf("sad download 2"),
	}}
	errMsg = e.Error()
	c.Check(strings.HasPrefix(errMsg, "cannot download:\n"), Equals, true)
	c.Check(errMsg, testutil.Contains, "\nsad download 1: \"foo\"")
	c.Check(errMsg, testutil.Contains, "\nsad download 2: \"bar\"")

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
	},
		Install: map[string]error{
			"bar": fmt.Errorf("sad install 2"),
		}}
	c.Check(e.Error(), Equals, `cannot refresh or install:
sad refresh 1: "foo"
sad install 2: "bar"`)

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
	},
		Download: map[string]error{
			"bar": fmt.Errorf("sad download 2"),
		}}
	c.Check(e.Error(), Equals, `cannot refresh or download:
sad refresh 1: "foo"
sad download 2: "bar"`)

	e = &store.SnapActionError{Install: map[string]error{
		"foo": fmt.Errorf("sad install 1"),
	},
		Download: map[string]error{
			"bar": fmt.Errorf("sad download 2"),
		}}
	c.Check(e.Error(), Equals, `cannot install or download:
sad install 1: "foo"
sad download 2: "bar"`)

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
	},
		Install: map[string]error{
			"bar": fmt.Errorf("sad install 2"),
		},
		Download: map[string]error{
			"baz": fmt.Errorf("sad download 3"),
		}}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download:
sad refresh 1: "foo"
sad install 2: "bar"
sad download 3: "baz"`)

	e = &store.SnapActionError{
		NoResults: true,
		Other:     []error{fmt.Errorf("other error")},
	}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download: other error`)

	e = &store.SnapActionError{
		Other: []error{fmt.Errorf("other error 1"), fmt.Errorf("other error 2")},
	}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download:
other error 1
other error 2`)

	e = &store.SnapActionError{
		Install: map[string]error{
			"bar": fmt.Errorf("sad install"),
		},
		Other: []error{fmt.Errorf("other error 1"), fmt.Errorf("other error 2")},
	}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download:
sad install: "bar"
other error 1
other error 2`)

	e = &store.SnapActionError{
		NoResults: true,
	}
	c.Check(e.Error(), Equals, "no install/refresh information results from the store")
}

func (s *storeTestSuite) TestSnapActionRefreshesBothAuths(c *C) {
	// snap action (install/refresh) has is its own custom way to
	// signal macaroon refreshes that allows to do a best effort
	// with the available results

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
	n := 0
	// mock store response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		switch r.URL.Path {
		case snapActionPath:
			n++
			type errObj struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			var errors []errObj

			authorization := r.Header.Get("Authorization")
			c.Check(authorization, Equals, s.expectedAuthorization(c, s.user))
			if s.user.StoreDischarges[0] != refresh {
				errors = append(errors, errObj{Code: "user-authorization-needs-refresh"})
			}

			devAuthorization := r.Header.Get("Snap-Device-Authorization")
			if devAuthorization == "" {
				c.Fatalf("device authentication missing")
			} else if devAuthorization == expiredAuth {
				errors = append(errors, errObj{Code: "device-authorization-needs-refresh"})
			} else {
				c.Check(devAuthorization, Equals, `Macaroon root="refreshed-session-macaroon"`)
			}

			errorsJSON, err := json.Marshal(errors)
			c.Assert(err, IsNil)

			io.WriteString(w, fmt.Sprintf(`{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "name": "canonical",
          "title": "Canonical"
       }
     }
  }],
  "error-list": %s
}`, errorsJSON))
		case authNoncesPath:
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case authSessionPath:
			// sanity of request
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
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, s.user, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Check(refreshDischargeEndpointHit, Equals, true)
	c.Check(refreshSessionRequested, Equals, true)
	c.Check(n, Equals, 2)
}

func (s *storeTestSuite) TestConnectivityCheckHappy(c *C) {
	seenPaths := make(map[string]int, 2)
	var mockServerURL *url.URL
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snaps/info/core":
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Query(), DeepEquals, url.Values{"fields": {"download"}, "architecture": {arch.UbuntuArchitecture()}})
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
		return
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
		return
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

func (s *storeTestSuite) TestSnapActionRefreshParallelInstall(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     fmt.Sprintf("%d-%s", 1, helloWorldSnapID),
			"revision":         float64(2),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": fmt.Sprintf("%d-%s", 1, helloWorldSnapID),
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "1-buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		}, {
			InstanceName:    "hello-world_foo",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world_foo",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].InstanceName(), Equals, "hello-world_foo")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeTestSuite) TestSnapActionRevisionNotAvailableParallelInstall(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     fmt.Sprintf("%d-%s", 1, helloWorldSnapID),
			"revision":         float64(2),
			"tracking-channel": "edge",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 3)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})
		c.Assert(req.Actions[1], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": fmt.Sprintf("%d-%s", 1, helloWorldSnapID),
			"snap-id":      helloWorldSnapID,
		})
		c.Assert(req.Actions[2], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "other",
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "error": {
       "code": "revision-not-found",
       "message": "msg1"
     }
  }, {
     "result": "error",
     "instance-key": "1-buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "error": {
       "code": "revision-not-found",
       "message": "msg2"
     }
  },  {
     "result": "error",
     "instance-key": "install-1",
     "snap-id": "foo-id",
     "name": "other",
     "error": {
       "code": "revision-not-found",
       "message": "msg3"
     }
  }
  ]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
		{
			InstanceName:    "hello-world_foo",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "edge",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			InstanceName: "hello-world",
			SnapID:       helloWorldSnapID,
		}, {
			Action:       "refresh",
			InstanceName: "hello-world_foo",
			SnapID:       helloWorldSnapID,
		}, {
			Action:       "install",
			InstanceName: "other_foo",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "stable",
			},
			"hello-world_foo": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "edge",
			},
		},
		Install: map[string]error{
			"other_foo": &store.RevisionNotAvailableError{
				Action:  "install",
				Channel: "stable",
			},
		},
	})
}

func (s *storeTestSuite) TestSnapActionInstallParallelInstall(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "hello-world",
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "install",
     "instance-key": "install-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "hello-world_foo",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world_foo")
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(28))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel is not set
	c.Assert(results[0].Channel, Equals, "")
}

func (s *storeTestSuite) TestSnapActionErrorsWhenNoInstanceName(c *C) {
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&store.Config{}, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:  "install",
			Channel: "stable",
		},
	}, nil, nil)
	c.Assert(err, ErrorMatches, "internal error: action without instance name")
	c.Assert(results, IsNil)
}

func (s *storeTestSuite) TestSnapActionInstallUnexpectedInstallKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "hello-world",
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "install",
     "instance-key": "foo-2",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "hello-world_foo",
			Channel:      "stable",
		},
	}, nil, nil)
	c.Assert(err, ErrorMatches, `unexpected invalid install/refresh API result: unexpected instance-key "foo-2"`)
	c.Assert(results, IsNil)
}

func (s *storeTestSuite) TestSnapActionRefreshUnexpectedInstanceKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "foo-5",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := store.New(&cfg, authContext)

	results, err := sto.SnapAction(context.TODO(), []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world",
		},
	}, nil, nil)
	c.Assert(err, ErrorMatches, `unexpected invalid install/refresh API result: unexpected refresh`)
	c.Assert(results, IsNil)
}
