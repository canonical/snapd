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

package store

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
	"github.com/snapcore/snapd/jsonutil/puritan"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func TestStore(t *testing.T) { TestingT(t) }

type configTestSuite struct{}

var _ = Suite(&configTestSuite{})

func (suite *configTestSuite) TestSetBaseURL(c *C) {
	// Sanity check to prove at least one URI changes.
	cfg := DefaultConfig()
	c.Assert(cfg.StoreBaseURL.String(), Equals, "https://api.snapcraft.io/")

	u, err := url.Parse("http://example.com/path/prefix/")
	c.Assert(err, IsNil)
	err = cfg.setBaseURL(u)
	c.Assert(err, IsNil)

	c.Check(cfg.StoreBaseURL.String(), Equals, "http://example.com/path/prefix/")
	c.Check(cfg.AssertionsBaseURL, IsNil)
}

func (suite *configTestSuite) TestSetBaseURLStoreOverrides(c *C) {
	cfg := DefaultConfig()
	c.Assert(cfg.setBaseURL(apiURL()), IsNil)
	c.Check(cfg.StoreBaseURL, Matches, apiURL().String()+".*")

	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "https://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")
	cfg = DefaultConfig()
	c.Assert(cfg.setBaseURL(apiURL()), IsNil)
	c.Check(cfg.StoreBaseURL.String(), Equals, "https://force-api.local/")
	c.Check(cfg.AssertionsBaseURL, IsNil)
}

func (suite *configTestSuite) TestSetBaseURLStoreURLBadEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://example.com"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")

	cfg := DefaultConfig()
	err := cfg.setBaseURL(apiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://example.com: missing protocol scheme")
}

func (suite *configTestSuite) TestSetBaseURLAssertsOverrides(c *C) {
	cfg := DefaultConfig()
	c.Assert(cfg.setBaseURL(apiURL()), IsNil)
	c.Check(cfg.AssertionsBaseURL, IsNil)

	c.Assert(os.Setenv("SNAPPY_FORCE_SAS_URL", "https://force-sas.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_SAS_URL", "")
	cfg = DefaultConfig()
	c.Assert(cfg.setBaseURL(apiURL()), IsNil)
	c.Check(cfg.AssertionsBaseURL, Matches, "https://force-sas.local/.*")
}

func (suite *configTestSuite) TestSetBaseURLAssertsURLBadEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_SAS_URL", "://example.com"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_SAS_URL", "")

	cfg := DefaultConfig()
	err := cfg.setBaseURL(apiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_SAS_URL: parse ://example.com: missing protocol scheme")
}

const (
	// Store API paths/patterns.
	authNoncesPath     = "/api/v1/snaps/auth/nonces"
	authSessionPath    = "/api/v1/snaps/auth/sessions"
	buyPath            = "/api/v1/snaps/purchases/buy"
	customersMePath    = "/api/v1/snaps/purchases/customers/me"
	detailsPathPattern = "/api/v1/snaps/details/.*"
	metadataPath       = "/api/v1/snaps/metadata"
	ordersPath         = "/api/v1/snaps/purchases/orders"
	searchPath         = "/api/v1/snaps/search"
	sectionsPath       = "/api/v1/snaps/sections"
)

// Build details path for a snap name.
func detailsPath(snapName string) string {
	return strings.Replace(detailsPathPattern, ".*", snapName, 1)
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
	store     *Store
	logbuf    *bytes.Buffer
	user      *auth.UserState
	localUser *auth.UserState
	device    *auth.DeviceState

	origDownloadFunc func(context.Context, string, string, string, *auth.UserState, *Store, io.ReadWriteSeeker, int64, progress.Meter) error
	mockXDelta       *testutil.MockCmd

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
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func makeTestDischarge() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("shared-key"), "third-party-caveat", UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func makeTestRefreshDischargeResponse() (string, error) {
	m, err := macaroon.New([]byte("shared-key"), "refreshed-third-party-caveat", UbuntuoneLocation)
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
	s.store = New(nil, nil)
	s.origDownloadFunc = download
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

	MockDefaultRetryStrategy(&s.BaseTest, retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	)))
}

func (s *storeTestSuite) TearDownTest(c *C) {
	download = s.origDownloadFunc
	s.mockXDelta.Restore()
	s.restoreLogger()
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
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Check(url, Equals, "anon-url")
		w.Write(expectedContent)
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContent))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeTestSuite) TestDownloadRangeRequest(c *C) {
	partialContentStr := "partial content "
	missingContentStr := "was downloaded"
	expectedContentStr := partialContentStr + missingContentStr

	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Check(resume, Equals, int64(len(partialContentStr)))
		c.Check(url, Equals, "anon-url")
		w.Write([]byte(missingContentStr))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = "abcdabcd"
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)

	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestResumeOfCompleted(c *C) {
	expectedContentStr := "nothing downloaded"

	download = nil

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = fmt.Sprintf("%x", sha3.Sum384([]byte(expectedContentStr)))
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(expectedContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
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
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
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
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
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
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
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
	err := s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)

	_, ok := err.(HashError)
	c.Assert(ok, Equals, true)
	// ensure we only retried once (as these downloads might be big)
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestDownloadRangeRequestRetryOnHashError(c *C) {
	expectedContentStr := "file was downloaded from scratch"
	partialContentStr := "partial content "

	n := 0
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		n++
		if n == 1 {
			// force sha3 error on first download
			c.Check(resume, Equals, int64(len(partialContentStr)))
			return HashError{"foo", "1234", "5678"}
		}
		w.Write([]byte(expectedContentStr))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = ""
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)

	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestDownloadRangeRequestFailOnHashError(c *C) {
	partialContentStr := "partial content "

	n := 0
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		n++
		return HashError{"foo", "1234", "5678"}
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = ""
	snap.Size = int64(len(partialContentStr) + 1)

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = s.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `sha3-384 mismatch for "foo": got 1234 but expected 5678`)
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestAuthenticatedDownloadDoesNotUseAnonURL(c *C) {
	expectedContent := []byte("I was downloaded")
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, _ *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		// check user is pass and auth url is used
		c.Check(user, Equals, s.user)
		c.Check(url, Equals, "AUTH-URL")

		w.Write(expectedContent)
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContent))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, s.user)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeTestSuite) TestAuthenticatedDeviceDoesNotUseAnonURL(c *C) {
	expectedContent := []byte("I was downloaded")
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		// check auth url is used
		c.Check(url, Equals, "AUTH-URL")

		w.Write(expectedContent)
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContent))

	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&Config{}, authContext)

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := sto.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeTestSuite) TestLocalUserDownloadUsesAnonURL(c *C) {
	expectedContentStr := "I was downloaded"
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Check(url, Equals, "anon-url")

		w.Write([]byte(expectedContentStr))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len(expectedContentStr))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, s.localUser)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContentStr)
}

func (s *storeTestSuite) TestDownloadFails(c *C) {
	var tmpfile *os.File
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		return fmt.Errorf("uh, it failed")
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = 1
	// simulate a failed download
	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, ErrorMatches, "uh, it failed")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (s *storeTestSuite) TestDownloadSyncFails(c *C) {
	var tmpfile *os.File
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		w.Write([]byte("sync will fail"))
		err := tmpfile.Close()
		c.Assert(err, IsNil)
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Size = int64(len("sync will fail"))

	// simulate a failed sync
	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, ErrorMatches, `(sync|fsync:) .*`)
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (s *storeTestSuite) TestActualDownload(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := New(&Config{}, nil)
	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 1)
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

	theStore := New(&Config{}, nil)

	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan string)
	go func() {
		sha3 := ""
		var buf SillyBuffer
		err := download(ctx, "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil)
		result <- err.Error()
		close(result)
	}()

	<-syncCh
	cancel()

	err := <-result
	c.Check(n, Equals, 1)
	c.Assert(err, Equals, "The download has been cancelled: context canceled")
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

	theStore := New(&Config{}, nil)
	var buf bytes.Buffer
	err := download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, nopeSeeker{&buf}, -1, nil)
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

	theStore := New(&Config{}, nil)
	var buf SillyBuffer
	err := download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &DownloadError{})
	c.Check(err.(*DownloadError).Code, Equals, 404)
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

	theStore := New(&Config{}, nil)
	var buf SillyBuffer
	err := download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &DownloadError{})
	c.Check(err.(*DownloadError).Code, Equals, 500)
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

	theStore := New(&Config{}, nil)
	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil)
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

	theStore := New(&Config{}, nil)
	buf := NewSillyBufferString("some ")
	// calc the expected hash
	h := crypto.SHA3_384.New()
	h.Write([]byte("some data"))
	sha3 := fmt.Sprintf("%x", h.Sum(nil))
	err := download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, buf, int64(len("some ")), nil)
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
		{env: "", classic: false, exeInHost: false, exeInCore: true, wantDelta: false},
		{env: "", classic: false, exeInHost: true, exeInCore: false, wantDelta: false},
		{env: "", classic: false, exeInHost: true, exeInCore: true, wantDelta: false},
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

		c.Check(useDeltas(), Equals, scenario.wantDelta, Commentf("%#v", scenario))
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
		download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
			if testCase.downloads[downloadIndex].error {
				downloadIndex++
				return errors.New("Bang")
			}
			c.Check(url, Equals, testCase.downloads[downloadIndex].url)
			w.Write([]byte(testCase.downloads[downloadIndex].url + "-content"))
			downloadIndex++
			return nil
		}
		applyDelta = func(name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
			c.Check(deltaInfo, Equals, &testCase.info.Deltas[0])
			err := ioutil.WriteFile(targetPath, []byte("snap-content-via-delta"), 0644)
			c.Assert(err, IsNil)
			return nil
		}

		path := filepath.Join(c.MkDir(), "subdir", "downloaded-file")
		err := s.store.Download(context.TODO(), "foo", path, &testCase.info, nil, nil)

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
	sto := New(nil, authContext)

	for _, testCase := range downloadDeltaTests {
		sto.deltaFormat = testCase.format
		download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, _ *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
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
		}

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

		err = sto.downloadDelta("snapname", &testCase.info, w, nil, authedUser)

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

		err = applyDelta(name, deltaPath, &testCase.deltaInfo, targetSnapPath, "")

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
	sto := New(&Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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
	sto := New(&Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.localUser)
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
	sto := New(&Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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
	UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

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
	sto := New(&Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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
	UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

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
	sto := New(&Config{}, authContext)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
	c.Assert(err, Equals, ErrInvalidCredentials)
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
	sto := New(&Config{
		StoreBaseURL: mockServerURL,
	}, authContext)

	reqOptions := &requestOptions{Method: "GET", URL: mockServerURL}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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
	UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

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
	sto := New(&Config{
		StoreBaseURL: mockServerURL,
	}, authContext)

	reqOptions := &requestOptions{Method: "GET", URL: mockServerURL}

	resp, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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

	sto := New(&Config{}, nil)
	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    endpoint,
		ExtraHeaders: map[string]string{
			"X-Foo-Header": "Bar",
			"Content-Type": "application/bson",
			"Accept":       "application/hal+bson",
			"User-Agent":   "customAgent",
		},
	}

	response, err := sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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
	MacaroonACLAPI = mockServer.URL + "/acl/"

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
	UbuntuoneDischargeAPI = mockSSOServer.URL + "/tokens/discharge"

	userMacaroon, userDischarge, err := LoginUser("username", "password", "otp")

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
	MacaroonACLAPI = mockServer.URL + "/acl/"

	userMacaroon, userDischarge, err := LoginUser("username", "password", "otp")

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
	MacaroonACLAPI = mockServer.URL + "/acl/"

	errorResponse := `{"code": "some-error"}`
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, errorResponse)
	}))
	c.Assert(mockSSOServer, NotNil)
	defer mockSSOServer.Close()
	UbuntuoneDischargeAPI = mockSSOServer.URL + "/tokens/discharge"

	userMacaroon, userDischarge, err := LoginUser("username", "password", "otp")

	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: .*")
	c.Check(userMacaroon, Equals, "")
	c.Check(userDischarge, Equals, "")
}

const (
	funkyAppName      = "8nzc1x4iim2xj1g2ul64"
	funkyAppDeveloper = "chipaca"
	funkyAppSnapID    = "1e21e12ex4iim2xj1g2ul6f12f1"

	helloWorldSnapID      = "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
	helloWorldDeveloperID = "canonical"
)

/* acquired via

http --pretty=format --print b https://api.snapcraft.io/api/v1/snaps/details/hello-world X-Ubuntu-Series:16 fields==anon_download_url,architecture,channel,download_sha3_384,summary,description,binary_filesize,download_url,icon_url,last_updated,license,package_name,prices,publisher,ratings_average,revision,screenshot_urls,snap_id,support_url,title,content,version,origin,developer_id,private,confinement,snap_yaml_raw channel==edge | xsel -b

on 2016-07-03. Then, by hand:
 * set prices to {"EUR": 0.99, "USD": 1.23}.
 * Screenshot URLS set manually.

on 2017-11-20. Then, by hand:
 * add "snap_yaml_raw" from "test-snapd-content-plug"

On Ubuntu, apt install httpie xsel (although you could get http from
the http snap instead).

*/
const MockDetailsJSON = `{
    "_links": {
        "self": {
            "href": "https://api.snapcraft.io/api/v1/snaps/details/hello-world?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Clicense%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement&channel=edge"
        }
    },
    "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
    "architecture": [
        "all"
    ],
    "base": "bare-base",
    "binary_filesize": 20480,
    "channel": "edge",
    "confinement": "strict",
    "content": "application",
    "description": "This is a simple hello world example.",
    "developer_id": "canonical",
    "download_sha3_384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
    "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
    "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
    "last_updated": "2016-07-12T16:37:23.960632Z",
    "license": "GPL-3.0",
    "origin": "canonical",
    "package_name": "hello-world",
    "prices": {"EUR": 0.99, "USD": 1.23},
    "publisher": "Canonical",
    "ratings_average": 0.0,
    "revision": 27,
    "screenshot_urls": ["https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/screenshot.png"],
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "summary": "The 'hello-world' of snaps",
    "support_url": "mailto:snappy-devel@lists.ubuntu.com",
    "title": "Hello World",
    "version": "6.3",
    "snap_yaml_raw": "name: test-snapd-content-plug\nversion: 1.0\napps:\n    content-plug:\n        command: bin/content-plug\n        plugs: [shared-content-plug]\nplugs:\n    shared-content-plug:\n        interface: content\n        target: import\n        content: mylib\n        default-provider: test-snapd-content-slot\nslots:\n    shared-content-slot:\n        interface: content\n        content: mylib\n        read:\n            - /\n",
    "channel_maps_list": [
      {
        "track": "latest",
        "map": [
          {
             "info": "released",
             "version": "v1",
             "binary_filesize": 12345,
             "epoch": "0",
             "confinement": "strict",
             "channel": "stable",
             "revision": 1
          },
          {
             "info": "released",
             "version": "v2",
             "binary_filesize": 12345,
             "epoch": "0",
             "confinement": "strict",
             "channel": "candidate",
             "revision": 2
          },
          {
             "info": "released",
             "version": "v8",
             "binary_filesize": 12345,
             "epoch": "0",
             "confinement": "devmode",
             "channel": "beta",
             "revision": 8
          },
          {
             "info": "released",
             "version": "v9",
             "binary_filesize": 12345,
             "epoch": "0",
             "confinement": "devmode",
             "channel": "edge",
             "revision": 9
          }
        ]
      }
    ]
}
`

// FIXME: this can go once the store always provides a channel_map_list
const MockDetailsJSONnoChannelMapList = `{
    "_links": {
        "self": {
            "href": "https://api.snapcraft.io/api/v1/snaps/details/hello-world?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Clicense%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement&channel=edge"
        }
    },
    "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
    "architecture": [
        "all"
    ],
    "binary_filesize": 20480,
    "channel": "edge",
    "confinement": "strict",
    "content": "application",
    "description": "This is a simple hello world example.",
    "developer_id": "canonical",
    "download_sha3_384": "eed62063c04a8c3819eb71ce7d929cc8d743b43be9e7d86b397b6d61b66b0c3a684f3148a9dbe5821360ae32105c1bd9",
    "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_27.snap",
    "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
    "last_updated": "2016-07-12T16:37:23.960632Z",
    "license": "GPL-3.0",
    "origin": "canonical",
    "package_name": "hello-world",
    "prices": {"EUR": 0.99, "USD": 1.23},
    "publisher": "Canonical",
    "ratings_average": 0.0,
    "revision": 27,
    "screenshot_urls": ["https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/screenshot.png"],
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "summary": "The 'hello-world' of snaps",
    "support_url": "mailto:snappy-devel@lists.ubuntu.com",
    "title": "Hello World",
    "version": "6.3"
}
`

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

func (s *storeTestSuite) TestDetails(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		c.Check(r.UserAgent(), Equals, userAgent)

		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Path, Matches, ".*/hello-world")

		c.Check(r.URL.Query().Get("channel"), Equals, "edge")
		c.Check(r.URL.Query().Get("fields"), Equals, "abc,def,snap_yaml_raw")

		c.Check(r.Header.Get("X-Ubuntu-Series"), Equals, release.Series)
		c.Check(r.Header.Get("X-Ubuntu-Architecture"), Equals, arch.UbuntuArchitecture())
		c.Check(r.Header.Get("X-Ubuntu-Classic"), Equals, "false")

		c.Check(r.Header.Get("X-Ubuntu-Confinement"), Equals, "")

		w.Header().Set("X-Suggested-Currency", "GBP")
		w.WriteHeader(200)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Architectures, DeepEquals, []string{"all"})
	c.Check(result.Revision, Equals, snap.R(27))
	c.Check(result.SnapID, Equals, helloWorldSnapID)
	c.Check(result.Publisher, Equals, "canonical")
	c.Check(result.Version, Equals, "6.3")
	c.Check(result.Sha3_384, Matches, `[[:xdigit:]]{96}`)
	c.Check(result.Size, Equals, int64(20480))
	c.Check(result.Channel, Equals, "edge")
	c.Check(result.Description(), Equals, "This is a simple hello world example.")
	c.Check(result.Summary(), Equals, "The 'hello-world' of snaps")
	c.Check(result.Title(), Equals, "Hello World")
	c.Check(result.License, Equals, "GPL-3.0")
	c.Assert(result.Prices, DeepEquals, map[string]float64{"EUR": 0.99, "USD": 1.23})
	c.Assert(result.Paid, Equals, true)
	c.Assert(result.Screenshots, DeepEquals, []snap.ScreenshotInfo{
		{
			URL: "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/screenshot.png",
		},
	})
	c.Check(result.MustBuy, Equals, true)
	c.Check(result.Contact, Equals, "mailto:snappy-devel@lists.ubuntu.com")
	c.Check(result.Base, Equals, "bare-base")

	// Make sure the epoch (currently not sent by the store) defaults to "0"
	c.Check(result.Epoch.String(), Equals, "0")

	c.Check(sto.SuggestedCurrency(), Equals, "GBP")

	// skip this one until the store supports it
	// c.Check(result.Private, Equals, true)

	c.Check(snap.Validate(result), IsNil)

	// validate the plugs/slots
	c.Check(result.Plugs, HasLen, 1)
	plug := result.Plugs["shared-content-plug"]
	c.Check(plug.Name, Equals, "shared-content-plug")
	c.Check(plug.Snap, DeepEquals, result)
	c.Check(plug.Apps, HasLen, 1)
	c.Check(plug.Apps["content-plug"].Command, Equals, "bin/content-plug")

	c.Check(result.Slots, HasLen, 1)
	slot := result.Slots["shared-content-slot"]
	c.Check(slot.Name, Equals, "shared-content-slot")
	c.Check(slot.Snap, DeepEquals, result)
	c.Check(slot.Apps, HasLen, 1)
	c.Check(slot.Apps["content-plug"].Command, Equals, "bin/content-plug")
}

func (s *storeTestSuite) TestDetailsDefaultChannelIsStable(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		c.Check(r.URL.Path, Matches, ".*/hello-world")

		c.Check(r.URL.Query().Get("channel"), Equals, "stable")
		w.WriteHeader(200)

		io.WriteString(w, strings.Replace(MockDetailsJSON, "edge", "stable", -1))
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	// the actual test
	spec := SnapSpec{
		Name: "hello-world",
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.SnapID, Equals, helloWorldSnapID)
	c.Check(result.Channel, Equals, "stable")
}

func (s *storeTestSuite) TestDetails500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	_, err := sto.SnapInfo(spec, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world" in channel "edge": got unexpected HTTP status code 500 via GET to "http://.*?/details/hello-world\?channel=edge"`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestDetails500once(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		n++
		if n > 1 {
			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(200)
			io.WriteString(w, MockDetailsJSON)
		} else {
			w.WriteHeader(500)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Assert(n, Equals, 2)
}

func (s *storeTestSuite) TestDetailsAndChannels(c *C) {
	// this test will break and should be melded into TestDetails,
	// above, when the store provides the channels as part of details

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		switch n {
		case 0:
			c.Check(r.URL.Path, Matches, ".*/hello-world")
			c.Check(r.URL.Query().Get("channel"), Equals, "")
			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(200)

			io.WriteString(w, MockDetailsJSON)
		default:
			c.Fatalf("unexpected request to %q", r.URL.Path)
		}
		n++
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	// the actual test
	spec := SnapSpec{
		Name:       "hello-world",
		AnyChannel: true,
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Channels, DeepEquals, map[string]*snap.ChannelSnapInfo{
		"latest/stable": {
			Revision:    snap.R(1),
			Version:     "v1",
			Confinement: snap.StrictConfinement,
			Channel:     "stable",
			Size:        12345,
			Epoch:       *snap.E("0"),
		},
		"latest/candidate": {
			Revision:    snap.R(2),
			Version:     "v2",
			Confinement: snap.StrictConfinement,
			Channel:     "candidate",
			Size:        12345,
			Epoch:       *snap.E("0"),
		},
		"latest/beta": {
			Revision:    snap.R(8),
			Version:     "v8",
			Confinement: snap.DevModeConfinement,
			Channel:     "beta",
			Size:        12345,
			Epoch:       *snap.E("0"),
		},
		"latest/edge": {
			Revision:    snap.R(9),
			Version:     "v9",
			Confinement: snap.DevModeConfinement,
			Channel:     "edge",
			Size:        12345,
			Epoch:       *snap.E("0"),
		},
	})

	c.Check(snap.Validate(result), IsNil)
}

func (s *storeTestSuite) TestNonDefaults(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	os.Setenv("SNAPPY_STORE_NO_CDN", "1")
	defer os.Unsetenv("SNAPPY_STORE_NO_CDN")

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "foo")

		c.Check(r.URL.Path, Matches, ".*/details/hello-world")

		c.Check(r.URL.Query().Get("channel"), Equals, "edge")

		c.Check(r.Header.Get("X-Ubuntu-Series"), Equals, "21")
		c.Check(r.Header.Get("X-Ubuntu-Architecture"), Equals, "archXYZ")
		c.Check(r.Header.Get("X-Ubuntu-Classic"), Equals, "true")
		// for now we have both
		c.Check(r.Header.Get("X-Ubuntu-No-CDN"), Equals, "true")
		c.Check(r.Header.Get("Snap-CDN"), Equals, "none")

		w.WriteHeader(200)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "foo"
	sto := New(cfg, nil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestStoreIDFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "my-brand-store-id")

		w.WriteHeader(200)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "fallback"
	sto := New(cfg, &testAuthContext{c: c, device: s.device, storeID: "my-brand-store-id"})

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestFullCloudInfoFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		c.Check(r.Header.Get("Snap-CDN"), Equals, `cloud-name="aws" region="us-east-1" availability-zone="us-east-1c"`)

		w.WriteHeader(200)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "fallback"
	sto := New(cfg, &testAuthContext{c: c, device: s.device, cloudInfo: &auth.CloudInfo{Name: "aws", Region: "us-east-1", AvailabilityZone: "us-east-1c"}})

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestLessDetailedCloudInfoFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		c.Check(r.Header.Get("Snap-CDN"), Equals, `cloud-name="openstack" availability-zone="nova"`)

		w.WriteHeader(200)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "fallback"
	sto := New(cfg, &testAuthContext{c: c, device: s.device, cloudInfo: &auth.CloudInfo{Name: "openstack", Region: "", AvailabilityZone: "nova"}})

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestProxyStoreFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)

		w.WriteHeader(200)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	nowhereURL, err := url.Parse("http://nowhere.invalid")
	c.Assert(err, IsNil)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = nowhereURL
	sto := New(cfg, &testAuthContext{
		c:             c,
		device:        s.device,
		proxyStoreID:  "foo",
		proxyStoreURL: mockServerURL,
	})

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestProxyStoreFromAuthContextURLFallback(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)

		w.WriteHeader(200)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	sto := New(cfg, &testAuthContext{
		c:      c,
		device: s.device,
		// mock an assertion that has id but no url
		proxyStoreID:  "foo",
		proxyStoreURL: nil,
	})

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestRevision(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ordersPath:
			w.WriteHeader(404)
		case detailsPath("hello-world"):
			c.Check(r.URL.Query(), DeepEquals, url.Values{
				"channel":  []string{""},
				"revision": []string{"26"},
			})
			w.WriteHeader(200)
			io.WriteString(w, MockDetailsJSON)
		default:
			c.Fatalf("unexpected request to %q", r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.DetailFields = []string{}
	sto := New(cfg, nil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(26),
	}
	result, err := sto.SnapInfo(spec, s.user)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Revision, DeepEquals, snap.R(27))
}

func (s *storeTestSuite) TestDetailsOopses(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		c.Check(r.URL.Path, Matches, ".*/hello-world")
		c.Check(r.URL.Query().Get("channel"), Equals, "edge")

		w.Header().Set("X-Oops-Id", "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f")
		w.WriteHeader(500)

		io.WriteString(w, `{"oops": "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	_, err := sto.SnapInfo(spec, nil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world" in channel "edge": got unexpected HTTP status code 5.. via GET to "http://\S+" \[OOPS-[[:xdigit:]]*\]`)
}

/*
acquired via

http --pretty=format --print b https://api.snapcraft.io/api/v1/snaps/details/no:such:package X-Ubuntu-Series:16 fields==anon_download_url,architecture,channel,download_sha512,summary,description,binary_filesize,download_url,icon_url,last_updated,license,package_name,prices,publisher,ratings_average,revision,snap_id,support_url,title,content,version,origin,developer_id,private,confinement channel==edge | xsel -b

on 2016-07-03

On Ubuntu, apt install httpie xsel (although you could get http from
the http snap instead).

*/
const MockNoDetailsJSON = `{
    "errors": [
        "No such package"
    ],
    "result": "error"
}`

func (s *storeTestSuite) TestNoDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		c.Check(r.URL.Path, Matches, ".*/no-such-pkg")

		q := r.URL.Query()
		c.Check(q.Get("channel"), Equals, "edge")
		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	// the actual test
	spec := SnapSpec{
		Name:     "no-such-pkg",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := sto.SnapInfo(spec, nil)
	c.Assert(err, NotNil)
	c.Assert(result, IsNil)
}

func (s *storeTestSuite) TestStructFields(c *C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(getStructFields(aStruct{}), DeepEquals, []string{"hello", "potato"})
}

func (s *storeTestSuite) TestStructFieldsExcept(c *C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(getStructFields(aStruct{}, "potato"), DeepEquals, []string{"hello"})
}

/* acquired via:
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://api.snapcraft.io/api/v1/snaps/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Clicense%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&q=hello' | python -m json.tool | xsel -b
Screenshot URLS set manually.
*/
const MockSearchJSON = `{
    "_embedded": {
        "clickindex:package": [
            {
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25.snap",
                "architecture": [
                    "all"
                ],
                "binary_filesize": 20480,
                "channel": "edge",
                "content": "application",
                "description": "This is a simple hello world example.",
                "download_sha512": "4bf23ce93efa1f32f0aeae7ec92564b7b0f9f8253a0bd39b2741219c1be119bb676c21208c6845ccf995e6aabe791d3f28a733ebcbbc3171bb23f67981f4068e",
                "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2016-04-19T19:50:50.435291Z",
                "license": "GPL-3.0",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {"EUR": 2.99, "USD": 3.49},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 25,
                "screenshot_urls": ["https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/screenshot.png"],
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "Hello World",
                "version": "6.0"
            }
        ]
    },
    "_links": {
        "first": {
            "href": "https://api.snapcraft.io/api/v1/snaps/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Clicense%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "last": {
            "href": "https://api.snapcraft.io/api/v1/snaps/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Clicense%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "self": {
            "href": "https://api.snapcraft.io/api/v1/snaps/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Clicense%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
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
			c.Check(section, Equals, "")
		case 1:
			c.Check(name, Equals, "")
			c.Check(q, Equals, "hello")
			c.Check(section, Equals, "")
		case 2:
			c.Check(name, Equals, "")
			c.Check(q, Equals, "")
			c.Check(section, Equals, "db")
		case 3:
			c.Check(name, Equals, "")
			c.Check(q, Equals, "hello")
			c.Check(section, Equals, "db")
		default:
			c.Fatalf("what? %d", n)
		}

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{"abc", "def"},
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	for _, query := range []Search{
		{Query: "hello", Prefix: true},
		{Query: "hello"},
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
	cfg := Config{
		StoreBaseURL: serverURL,
	}
	sto := New(&cfg, nil)

	sections, err := sto.Sections(s.user)
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
        "package_name": "bar"
      },
      {
        "aliases": [{"name": "meh", "target": "foo"}],
        "apps": ["foo"],
        "package_name": "foo"
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
	sto := New(&Config{StoreBaseURL: serverURL}, nil)

	db, err := advisor.Create()
	c.Assert(err, IsNil)
	defer db.Rollback()

	var bufNames bytes.Buffer
	err = sto.WriteCatalogs(&bufNames, db)
	c.Assert(err, IsNil)
	db.Commit()
	c.Check(bufNames.String(), Equals, "bar\nfoo\n")

	dump, err := advisor.DumpCommands()
	c.Assert(err, IsNil)
	c.Check(dump, DeepEquals, map[string][]string{
		"foo":     {"foo"},
		"bar.baz": {"bar"},
		"potato":  {"bar"},
		"meh":     {"bar", "foo"},
	})
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
	cfg := Config{
		StoreBaseURL: serverURL,
	}
	sto := New(&cfg, nil)

	_, err := sto.Find(&Search{Query: "foo", Private: true}, s.user)
	c.Check(err, IsNil)

	_, err = sto.Find(&Search{Query: "foo", Private: true}, nil)
	c.Check(err, Equals, ErrUnauthenticated)

	_, err = sto.Find(&Search{Query: "name:foo", Private: true}, s.user)
	c.Check(err, Equals, ErrBadQuery)
}

func (s *storeTestSuite) TestFindFailures(c *C) {
	sto := New(&Config{StoreBaseURL: new(url.URL)}, nil)
	_, err := sto.Find(&Search{Query: "foo:bar"}, nil)
	c.Check(err, Equals, ErrBadQuery)
	_, err = sto.Find(&Search{Query: "foo", Private: true, Prefix: true}, s.user)
	c.Check(err, Equals, ErrBadQuery)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := New(&cfg, nil)

	snaps, err := sto.Find(&Search{Query: "hello"}, nil)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := New(&cfg, nil)

	snaps, err := sto.Find(&Search{Query: "hello"}, nil)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := New(&cfg, nil)

	snaps, err := sto.Find(&Search{Query: "hello"}, nil)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
	}
	sto := New(&cfg, nil)

	_, err := sto.Find(&Search{Query: "hello"}, nil)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{},
	}
	sto := New(&cfg, nil)

	snaps, err := sto.Find(&Search{Query: "hello"}, nil)
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
			c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
		DetailFields: []string{}, // make the error less noisy
	}
	sto := New(&cfg, nil)

	snaps, err := sto.Find(&Search{Query: "foo"}, s.user)
	c.Assert(err, IsNil)

	// Check that we log an error.
	c.Check(s.logbuf.String(), Matches, "(?ms).* cannot get user orders: invalid credentials")

	// But still successfully return snap information.
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].SnapID, Equals, helloWorldSnapID)
	c.Check(snaps[0].Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Check(snaps[0].MustBuy, Equals, true)
}

func (s *storeTestSuite) TestCurrentSnap(c *C) {
	cand := &RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(1),
		Epoch:    *snap.E("1"),
	}
	cs := currentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Channel, Equals, cand.Channel)
	c.Check(cs.Epoch, DeepEquals, cand.Epoch)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(cs.IgnoreValidation, Equals, cand.IgnoreValidation)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapIgnoreValidation(c *C) {
	cand := &RefreshCandidate{
		SnapID:           helloWorldSnapID,
		Channel:          "stable",
		Revision:         snap.R(1),
		Epoch:            *snap.E("1"),
		IgnoreValidation: true,
	}
	cs := currentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Channel, Equals, cand.Channel)
	c.Check(cs.Epoch, DeepEquals, cand.Epoch)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(cs.IgnoreValidation, Equals, cand.IgnoreValidation)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapNoChannel(c *C) {
	cand := &RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Revision: snap.R(1),
		Epoch:    *snap.E("1"),
	}
	cs := currentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Channel, Equals, "stable")
	c.Check(cs.Epoch, DeepEquals, cand.Epoch)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapNilNoID(c *C) {
	cand := &RefreshCandidate{
		SnapID:   "",
		Revision: snap.R(1),
	}
	cs := currentSnap(cand)
	c.Assert(cs, IsNil)
	c.Check(s.logbuf.String(), Matches, "(?m).* an empty SnapID but a store revision!")
}

func (s *storeTestSuite) TestCurrentSnapNilLocalRevision(c *C) {
	cand := &RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Revision: snap.R("x1"),
	}
	cs := currentSnap(cand)
	c.Assert(cs, IsNil)
	c.Check(s.logbuf.String(), Matches, "(?m).* a non-empty SnapID but a non-store revision!")
}

func (s *storeTestSuite) TestCurrentSnapNilLocalRevisionNoID(c *C) {
	cand := &RefreshCandidate{
		SnapID:   "",
		Revision: snap.R("x1"),
	}
	cs := currentSnap(cand)
	c.Assert(cs, IsNil)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *storeTestSuite) TestCurrentSnapRevLocalRevWithAmendHappy(c *C) {
	cand := &RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Revision: snap.R("x1"),
		Amend:    true,
	}
	cs := currentSnap(cand)
	c.Assert(cs, NotNil)
	c.Check(cs.SnapID, Equals, cand.SnapID)
	c.Check(cs.Revision, Equals, cand.Revision.N)
	c.Check(s.logbuf.String(), Equals, "")
}

/* acquired via:
(against production "hello-world")
$ curl -s --data-binary '{"snaps":[{"snap_id":"buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ","channel":"stable","revision":25,"epoch":"0","confinement":"strict"}],"fields":["anon_download_url","architecture","channel","download_sha512","summary","description","binary_filesize","download_url","icon_url","last_updated","license","package_name","prices","publisher","ratings_average","revision","snap_id","support_url","title","content","version","origin","developer_id","private","confinement"]}'  -H 'content-type: application/json' -H 'X-Ubuntu-Release: 16' -H 'X-Ubuntu-Wire-Protocol: 1' -H "accept: application/hal+json" https://api.snapcraft.io/api/v1/snaps/metadata | python3 -m json.tool --sort-keys | xsel -b
*/
var MockUpdatesJSON = `
{
    "_embedded": {
        "clickindex:package": [
            {
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
                "architecture": [
                    "all"
                ],
                "binary_filesize": 20480,
                "channel": "stable",
                "confinement": "strict",
                "content": "application",
                "description": "This is a simple hello world example.",
                "developer_id": "canonical",
                "download_sha512": "345f33c06373f799b64c497a778ef58931810dd7ae85279d6917d8b4f43d38abaf37e68239cb85914db276cb566a0ef83ea02b6f2fd064b54f9f2508fa4ca1f1",
                "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2016-05-31T07:02:32.586839Z",
                "license": "GPL-3.0",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 26,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "Hello World",
                "version": "6.1"
            }
        ]
    }
}
`

func (s *storeTestSuite) TestRefreshForCandidates(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(1),
			"epoch":       "0",
			"confinement": "",
		})
		c.Assert(resp.Fields, DeepEquals, detailFields)

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	results, err := sto.refreshForCandidates([]*currentSnapJSON{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: 1,
		},
	}, nil, nil)

	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name.Clean(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, 26)
	c.Assert(results[0].Version.Clean(), Equals, "6.1")
	c.Assert(results[0].SnapID.Clean(), Equals, helloWorldSnapID)
	c.Assert(results[0].DeveloperID.Clean(), Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestRefreshForCandidatesRetriesOnEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		n++
		if n < 4 {
			io.WriteString(w, "{")
			mockServer.CloseClientConnections()
			return
		}
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}
		err := json.NewDecoder(r.Body).Decode(&resp)
		c.Assert(err, IsNil)
		c.Assert(resp.Snaps, HasLen, 1)
		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	results, err := sto.refreshForCandidates([]*currentSnapJSON{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: 1,
	}}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name.Clean(), Equals, "hello-world")
}

func mockRFC(newRFC func(*Store, []*currentSnapJSON, *auth.UserState, *RefreshOptions) ([]*snapDetails, error)) func() {
	oldRFC := refreshForCandidates
	refreshForCandidates = newRFC
	return func() {
		refreshForCandidates = oldRFC
	}
}

func (s *storeTestSuite) TestLookupRefresh(c *C) {
	defer mockRFC(func(_ *Store, currentSnaps []*currentSnapJSON, _ *auth.UserState, _ *RefreshOptions) ([]*snapDetails, error) {
		c.Check(currentSnaps, DeepEquals, []*currentSnapJSON{{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: 1,
			Epoch:    *snap.E("0"),
		}})
		return []*snapDetails{{
			Name:        puritan.NewSimpleString("hello-world"),
			Revision:    26,
			Version:     puritan.NewString("6.1"),
			SnapID:      puritan.NewSimpleString(helloWorldSnapID),
			DeveloperID: puritan.NewSimpleString(helloWorldDeveloperID),
		}}, nil
	})()

	sto := New(nil, &testAuthContext{c: c, device: s.device})

	result, err := sto.LookupRefresh(&RefreshCandidate{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(1),
		Epoch:    *snap.E("0"),
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(result.Name(), Equals, "hello-world")
	c.Assert(result.Revision, Equals, snap.R(26))
	c.Assert(result.Version, Equals, "6.1")
	c.Assert(result.SnapID, Equals, helloWorldSnapID)
	c.Assert(result.PublisherID, Equals, helloWorldDeveloperID)
	c.Assert(result.Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestLookupRefreshIgnoreValidation(c *C) {
	defer mockRFC(func(_ *Store, currentSnaps []*currentSnapJSON, _ *auth.UserState, _ *RefreshOptions) ([]*snapDetails, error) {
		c.Check(currentSnaps, DeepEquals, []*currentSnapJSON{{
			SnapID:           helloWorldSnapID,
			Channel:          "stable",
			Revision:         1,
			Epoch:            *snap.E("0"),
			IgnoreValidation: true,
		}})
		return []*snapDetails{{
			Name:        puritan.NewSimpleString("hello-world"),
			Revision:    26,
			Version:     puritan.NewString("6.1"),
			SnapID:      puritan.NewSimpleString(helloWorldSnapID),
			DeveloperID: puritan.NewSimpleString(helloWorldDeveloperID),
		}}, nil
	})()

	sto := New(nil, &testAuthContext{c: c, device: s.device})

	result, err := sto.LookupRefresh(&RefreshCandidate{
		SnapID:           helloWorldSnapID,
		Channel:          "stable",
		Revision:         snap.R(1),
		Epoch:            *snap.E("0"),
		IgnoreValidation: true,
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(result.Name(), Equals, "hello-world")
	c.Assert(result.Revision, Equals, snap.R(26))
	c.Assert(result.SnapID, Equals, helloWorldSnapID)
}

func (s *storeTestSuite) TestLookupRefreshLocalSnap(c *C) {
	defer mockRFC(func(_ *Store, _ []*currentSnapJSON, _ *auth.UserState, _ *RefreshOptions) ([]*snapDetails, error) {
		panic("unexpected call to refreshForCandidates")
	})()

	sto := New(nil, &testAuthContext{c: c, device: s.device})

	result, err := sto.LookupRefresh(&RefreshCandidate{
		Revision: snap.R("x1"),
	}, nil)
	c.Assert(result, IsNil)
	c.Check(err, Equals, ErrLocalSnap)
}

func (s *storeTestSuite) TestLookupRefreshRFCError(c *C) {
	anError := errors.New("ouchie")
	defer mockRFC(func(_ *Store, _ []*currentSnapJSON, _ *auth.UserState, _ *RefreshOptions) ([]*snapDetails, error) {
		return nil, anError
	})()

	sto := New(nil, &testAuthContext{c: c, device: s.device})

	result, err := sto.LookupRefresh(&RefreshCandidate{
		SnapID:   helloWorldDeveloperID,
		Revision: snap.R(1),
	}, nil)
	c.Assert(result, IsNil)
	c.Check(err, Equals, anError)
}

func (s *storeTestSuite) TestLookupRefreshEmptyResponse(c *C) {
	defer mockRFC(func(_ *Store, _ []*currentSnapJSON, _ *auth.UserState, _ *RefreshOptions) ([]*snapDetails, error) {
		return nil, nil
	})()

	sto := New(nil, &testAuthContext{c: c, device: s.device})

	result, err := sto.LookupRefresh(&RefreshCandidate{
		SnapID:   helloWorldDeveloperID,
		Revision: snap.R(1),
	}, nil)
	c.Assert(result, IsNil)
	c.Check(err, Equals, ErrSnapNotFound)
}

func (s *storeTestSuite) TestLookupRefreshNoUpdate(c *C) {
	defer mockRFC(func(_ *Store, _ []*currentSnapJSON, _ *auth.UserState, _ *RefreshOptions) ([]*snapDetails, error) {
		return []*snapDetails{{
			SnapID:   puritan.NewSimpleString(helloWorldDeveloperID),
			Revision: 1,
		}}, nil
	})()

	sto := New(nil, &testAuthContext{c: c, device: s.device})

	result, err := sto.LookupRefresh(&RefreshCandidate{
		SnapID:   helloWorldDeveloperID,
		Revision: snap.R(1),
	}, nil)
	c.Assert(result, IsNil)
	c.Check(err, Equals, ErrNoUpdateAvailable)
}

func (s *storeTestSuite) TestListRefresh(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(1),
			"epoch":       "0",
			"confinement": "",
		})
		c.Assert(resp.Fields, DeepEquals, detailFields)

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	results, err := sto.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(1),
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].PublisherID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestListRefreshIgnoreValidation(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":           helloWorldSnapID,
			"channel":           "stable",
			"revision":          float64(1),
			"epoch":             "0",
			"confinement":       "",
			"ignore_validation": true,
		})
		c.Assert(resp.Fields, DeepEquals, detailFields)

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	results, err := sto.ListRefresh([]*RefreshCandidate{
		{
			SnapID:           helloWorldSnapID,
			Channel:          "stable",
			Revision:         snap.R(1),
			IgnoreValidation: true,
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
}

func (s *storeTestSuite) TestListRefreshDefaultChannelIsStable(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(1),
			"epoch":       "0",
			"confinement": "",
		})
		c.Assert(resp.Fields, DeepEquals, detailFields)

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	results, err := sto.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Revision: snap.R(1),
		},
	}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].PublisherID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestListRefreshRetryOnEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		n++
		if n < 4 {
			io.WriteString(w, "{")
			mockServer.CloseClientConnections()
			return
		}
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}
		err := json.NewDecoder(r.Body).Decode(&resp)
		c.Assert(err, IsNil)
		c.Assert(resp.Snaps, HasLen, 1)
		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	results, err := sto.ListRefresh([]*RefreshCandidate{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(1),
	}}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
}

func (s *storeTestSuite) TestUnexpectedEOFhandling(c *C) {
	permanentlyBrokenSrvCalls := 0
	somewhatBrokenSrvCalls := 0

	mockPermanentlyBrokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		permanentlyBrokenSrvCalls++
		w.Header().Add("Content-Length", "1000")
	}))
	mockSomewhatBrokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		somewhatBrokenSrvCalls++
		if somewhatBrokenSrvCalls > 3 {
			io.WriteString(w, MockUpdatesJSON)
			return
		}
		w.Header().Add("Content-Length", "1000")
	}))

	queryServer := func(mockServer *httptest.Server) error {
		c.Assert(mockServer, NotNil)
		defer mockServer.Close()

		mockServerURL, _ := url.Parse(mockServer.URL)
		cfg := Config{
			StoreBaseURL: mockServerURL,
		}
		authContext := &testAuthContext{c: c, device: s.device}
		sto := New(&cfg, authContext)

		_, err := sto.refreshForCandidates([]*currentSnapJSON{{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: 1,
		}}, nil, nil)
		return err
	}

	// Check that we really recognize unexpected EOF error by failing on all retries
	err := queryServer(mockPermanentlyBrokenServer)
	c.Assert(err, NotNil)
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(err, ErrorMatches, "unexpected EOF")
	// check that we exhausted all retries (as defined by mocked retry strategy)
	c.Assert(permanentlyBrokenSrvCalls, Equals, 5)

	// Check that we retry on unexpected EOF and eventually succeed
	err = queryServer(mockSomewhatBrokenServer)
	c.Assert(err, IsNil)
	// check that we retried 4 times
	c.Assert(somewhatBrokenSrvCalls, Equals, 4)
}

func (s *storeTestSuite) TestRefreshForCandidatesEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		n++
		io.WriteString(w, "{")
		mockServer.CloseClientConnections()
		return
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	_, err := sto.refreshForCandidates([]*currentSnapJSON{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: 1,
	}}, nil, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `^Post http://127.0.0.1:.*?/metadata: EOF$`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestRefreshForCandidatesUnauthorised(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		n++
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		w.WriteHeader(401)
		io.WriteString(w, "")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}

	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	_, err := sto.refreshForCandidates([]*currentSnapJSON{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: 24,
	}}, nil, nil)
	c.Assert(n, Equals, 1)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 401 via POST to "http://.*?/metadata"`)
}

func (s *storeTestSuite) TestRefreshForCandidatesFailOnDNS(c *C) {
	baseURL, err := url.Parse("http://nonexistingserver909123.com/")
	c.Assert(err, IsNil)
	cfg := Config{
		StoreBaseURL: baseURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	_, err = sto.refreshForCandidates([]*currentSnapJSON{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: 24,
	}}, nil, nil)
	// the error differs depending on whether a proxy is in use (e.g. on travis), so don't inspect error message
	c.Assert(err, NotNil)
}

func (s *storeTestSuite) TestRefreshForCandidates500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		n++
		w.WriteHeader(500)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	_, err := sto.refreshForCandidates([]*currentSnapJSON{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: 24,
	}}, nil, nil)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 500 via POST to "http://.*?/metadata"`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestRefreshForCandidates500DurationExceeded(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		n++
		time.Sleep(time.Duration(2) * time.Second)
		w.WriteHeader(500)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

	_, err := sto.refreshForCandidates([]*currentSnapJSON{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: 24,
	}}, nil, nil)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 500 via POST to "http://.*?/metadata"`)
	c.Assert(n, Equals, 1)
}

func (s *storeTestSuite) TestAcceptableUpdateWorks(c *C) {
	c.Check(acceptableUpdate(&snapDetails{Revision: 42}, &RefreshCandidate{Revision: snap.R("1")}), Equals, true)
}
func (s *storeTestSuite) TestAcceptableUpdateSkipsCurrent(c *C) {
	c.Check(acceptableUpdate(&snapDetails{Revision: 42}, &RefreshCandidate{Revision: snap.R("42")}), Equals, false)
}
func (s *storeTestSuite) TestAcceptableUpdateSkipsBlocked(c *C) {
	c.Check(acceptableUpdate(&snapDetails{Revision: 42}, &RefreshCandidate{Revision: snap.R("1"), Block: []snap.Revision{snap.R("42")}}), Equals, false)
}
func (s *storeTestSuite) TestAcceptableUpdateSkipsBoth(c *C) {
	// belts-and-suspenders
	c.Check(acceptableUpdate(&snapDetails{Revision: 42}, &RefreshCandidate{Revision: snap.R("42"), Block: []snap.Revision{snap.R("42")}}), Equals, false)
}

func (s *storeTestSuite) TestListRefreshSkipCurrent(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps []map[string]interface{} `json:"snaps"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(26),
			"epoch":       "0",
			"confinement": "",
		})

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	results, err := sto.ListRefresh([]*RefreshCandidate{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(26),
	}}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (s *storeTestSuite) TestListRefreshSkipBlocked(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)

		var resp struct {
			Snaps []map[string]interface{} `json:"snaps"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(25),
			"epoch":       "0",
			"confinement": "",
		})

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	results, err := sto.ListRefresh([]*RefreshCandidate{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(25),
		Block:    []snap.Revision{snap.R(26)},
	}}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

/* XXX Currently this is just MockUpdatesJSON with the deltas that we're
planning to add to the stores /api/v1/snaps/metadata response.
*/
var MockUpdatesWithDeltasJSON = `
{
    "_embedded": {
        "clickindex:package": [
            {
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
                "architecture": [
                    "all"
                ],
                "binary_filesize": 20480,
                "channel": "stable",
                "confinement": "strict",
                "content": "application",
                "description": "This is a simple hello world example.",
                "developer_id": "canonical",
                "download_sha512": "345f33c06373f799b64c497a778ef58931810dd7ae85279d6917d8b4f43d38abaf37e68239cb85914db276cb566a0ef83ea02b6f2fd064b54f9f2508fa4ca1f1",
                "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2016-05-31T07:02:32.586839Z",
                "license": "GPL-3.0",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 26,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "Hello World",
                "version": "6.1",
                "deltas": [{
                    "from_revision": 24,
                    "to_revision": 25,
                    "format": "xdelta3",
                    "binary_filesize": 204,
                    "download_sha3_384": "sha3_384_hash",
                    "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_24_25_xdelta3.delta",
                    "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_24_25_xdelta3.delta"
                }, {
                    "from_revision": 25,
                    "to_revision": 26,
                    "format": "xdelta3",
                    "binary_filesize": 206,
                    "download_sha3_384": "sha3_384_hash",
                    "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25_26_xdelta3.delta",
                    "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25_26_xdelta3.delta"
                }]
            }
        ]
    }
}
`

func (s *storeTestSuite) TestDefaultsDeltasOnClassicOnly(c *C) {
	for _, t := range []struct {
		onClassic      bool
		deltaFormatStr string
	}{
		{false, ""},
		{true, "xdelta3"},
	} {
		restore := release.MockOnClassic(t.onClassic)
		defer restore()

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertRequest(c, r, "POST", metadataPath)
			c.Check(r.Header.Get("X-Ubuntu-Delta-Formats"), Equals, t.deltaFormatStr)
		}))
		defer mockServer.Close()

		mockServerURL, _ := url.Parse(mockServer.URL)
		cfg := Config{
			StoreBaseURL: mockServerURL,
		}
		sto := New(&cfg, nil)

		sto.refreshForCandidates([]*currentSnapJSON{{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: 1,
		}}, nil, nil)
	}
}

func (s *storeTestSuite) TestListRefreshWithDeltas(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		c.Check(r.Header.Get("X-Ubuntu-Delta-Formats"), Equals, `xdelta3`)
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(24),
			"epoch":       "0",
			"confinement": "",
		})
		c.Assert(resp.Fields, DeepEquals, getStructFields(snapDetails{}, "snap_yaml_raw"))

		io.WriteString(w, MockUpdatesWithDeltasJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	results, err := sto.ListRefresh([]*RefreshCandidate{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(24),
	}}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Deltas, HasLen, 2)
	c.Assert(results[0].Deltas[0], Equals, snap.DeltaInfo{
		FromRevision:    24,
		ToRevision:      25,
		Format:          "xdelta3",
		AnonDownloadURL: "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_24_25_xdelta3.delta",
		DownloadURL:     "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_24_25_xdelta3.delta",
		Size:            204,
		Sha3_384:        "sha3_384_hash",
	})
	c.Assert(results[0].Deltas[1], Equals, snap.DeltaInfo{
		FromRevision:    25,
		ToRevision:      26,
		Format:          "xdelta3",
		AnonDownloadURL: "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25_26_xdelta3.delta",
		DownloadURL:     "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25_26_xdelta3.delta",
		Size:            206,
		Sha3_384:        "sha3_384_hash",
	})
}

func (s *storeTestSuite) TestListRefreshWithoutDeltas(c *C) {
	// Verify the X-Delta-Format header is not set.
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "0"), IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", metadataPath)
		c.Check(r.Header.Get("X-Ubuntu-Delta-Formats"), Equals, ``)
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(24),
			"epoch":       "0",
			"confinement": "",
		})
		c.Assert(resp.Fields, DeepEquals, detailFields)

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	results, err := sto.ListRefresh([]*RefreshCandidate{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(24),
	}}, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeTestSuite) TestUpdateNotSendLocalRevs(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Error(r.URL.Path)
		c.Fatal("no network request expected")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	_, err := sto.ListRefresh([]*RefreshCandidate{{
		SnapID:   helloWorldSnapID,
		Channel:  "stable",
		Revision: snap.R(-2),
	}}, nil, nil)
	c.Assert(err, IsNil)
}

func (s *storeTestSuite) TestListRefreshOptions(c *C) {
	for _, t := range []struct {
		flag   *RefreshOptions
		header string
	}{
		{nil, ""},
		{&RefreshOptions{RefreshManaged: true}, "X-Ubuntu-Refresh-Managed"},
	} {

		mockServerHit := false
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertRequest(c, r, "POST", metadataPath)
			if t.header != "" {
				c.Check(r.Header.Get(t.header), Equals, "true")
			}
			mockServerHit = true
			io.WriteString(w, `{}`)
		}))

		c.Assert(mockServer, NotNil)
		defer mockServer.Close()

		mockServerURL, _ := url.Parse(mockServer.URL)
		cfg := Config{
			StoreBaseURL: mockServerURL,
		}
		sto := New(&cfg, nil)

		_, err := sto.ListRefresh([]*RefreshCandidate{{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(24),
		}}, nil, t.flag)
		c.Assert(err, IsNil)
		c.Check(mockServerHit, Equals, true)
	}
}

func (s *storeTestSuite) TestStructFieldsSurvivesNoTag(c *C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int
	}
	c.Assert(getStructFields(aStruct{}), DeepEquals, []string{"hello"})
}

func (s *storeTestSuite) TestAuthLocationDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := authLocation()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := authLocation()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestAuthURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := authURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := authURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestApiURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := apiURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := apiURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestStoreURLDependsOnEnviron(c *C) {
	// This also depends on the API URL, but that's tested separately (see
	// TestApiURLDependsOnEnviron).
	api := apiURL()

	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", ""), IsNil)
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", ""), IsNil)

	// Test in order of precedence (low first) leaving env vars set as we go ...

	u, err := storeURL(api)
	c.Assert(err, IsNil)
	c.Check(u.String(), Matches, api.String()+".*")

	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "https://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")
	u, err = storeURL(api)
	c.Assert(err, IsNil)
	c.Check(u.String(), Matches, "https://force-api.local/.*")

	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", "https://force-cpi.local/api/v1/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_CPI_URL", "")
	u, err = storeURL(api)
	c.Assert(err, IsNil)
	c.Check(u.String(), Matches, "https://force-cpi.local/.*")
}

func (s *storeTestSuite) TestStoreURLBadEnvironAPI(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")
	_, err := storeURL(apiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://force-api.local/: missing protocol scheme")
}

func (s *storeTestSuite) TestStoreURLBadEnvironCPI(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_CPI_URL", "://force-cpi.local/api/v1/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_CPI_URL", "")
	_, err := storeURL(apiURL())
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_CPI_URL: parse ://force-cpi.local/: missing protocol scheme")
}

func (s *storeTestSuite) TestStoreDeveloperURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := storeDeveloperURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := storeDeveloperURL()

	c.Check(before, Not(Equals), after)
}

func (s *storeTestSuite) TestDefaultConfig(c *C) {
	c.Check(defaultConfig.StoreBaseURL.String(), Equals, "https://api.snapcraft.io/")
	c.Check(defaultConfig.AssertionsBaseURL, IsNil)
}

func (s *storeTestSuite) TestNew(c *C) {
	aStore := New(nil, nil)
	c.Assert(aStore, NotNil)
	// check for fields
	c.Check(aStore.detailFields, DeepEquals, detailFields)
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	authContext := &testAuthContext{c: c, device: s.device}
	sto := New(&cfg, authContext)

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
	cfg := Config{
		AssertionsBaseURL: nowhereURL,
	}
	authContext := &testAuthContext{
		c:             c,
		device:        s.device,
		proxyStoreID:  "foo",
		proxyStoreURL: mockServerURL,
	}
	sto := New(&cfg, authContext)

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
	cfg := Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

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
	cfg := Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	_, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, ErrorMatches, `cannot fetch assertion: got unexpected HTTP status code 500 via .+`)
	c.Assert(n, Equals, 5)
}

func (s *storeTestSuite) TestSuggestedCurrency(c *C) {
	suggestedCurrency := "GBP"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", detailsPathPattern)
		w.Header().Set("X-Suggested-Currency", suggestedCurrency)
		w.WriteHeader(200)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

	// the store doesn't know the currency until after the first search, so fall back to dollars
	c.Check(sto.SuggestedCurrency(), Equals, "USD")

	// we should soon have a suggested currency
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
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
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockOrdersJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, authContext)

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

	err := sto.decorateOrders(snaps, s.user)
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
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(401)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, nil)

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

	err := sto.decorateOrders(snaps, s.user)
	c.Assert(err, NotNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersNoAuth(c *C) {
	cfg := Config{}
	sto := New(&cfg, nil)

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

	err := sto.decorateOrders(snaps, nil)
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
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		requestRecieved = true
		io.WriteString(w, `{"orders": []}`)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}

	sto := New(&cfg, nil)

	// This snap is free
	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	// This snap is also free
	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID

	snaps := []*snap.Info{helloWorld, funkyApp}

	// There should be no request to the purchase server.
	err := sto.decorateOrders(snaps, s.user)
	c.Assert(err, IsNil)
	c.Check(requestRecieved, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersSingle(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockSingleOrderJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, authContext)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	snaps := []*snap.Info{helloWorld}

	err := sto.decorateOrders(snaps, s.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersSingleFreeSnap(c *C) {
	cfg := Config{}
	sto := New(&cfg, nil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	snaps := []*snap.Info{helloWorld}

	err := sto.decorateOrders(snaps, s.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (s *storeTestSuite) TestDecorateOrdersSingleNotFound(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", ordersPath)
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(404)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, authContext)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	snaps := []*snap.Info{helloWorld}

	err := sto.decorateOrders(snaps, s.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (s *storeTestSuite) TestDecorateOrdersTokenExpired(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(401)
		io.WriteString(w, "")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	mockServerURL, _ := url.Parse(mockPurchasesServer.URL)
	authContext := &testAuthContext{c: c, device: s.device, user: s.user}
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, authContext)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}
	helloWorld.Paid = true

	snaps := []*snap.Info{helloWorld}

	err := sto.decorateOrders(snaps, s.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (s *storeTestSuite) TestMustBuy(c *C) {
	// Never need to buy a free snap.
	c.Check(mustBuy(false, true), Equals, false)
	c.Check(mustBuy(false, false), Equals, false)

	// Don't need to buy snaps that have been bought.
	c.Check(mustBuy(true, true), Equals, false)

	// Need to buy snaps that aren't bought.
	c.Check(mustBuy(true, false), Equals, true)
}

const customersMeValid = `
{
  "latest_tos_date": "2016-09-14T00:00:00+00:00",
  "accepted_tos_date": "2016-09-14T15:56:49+00:00",
  "latest_tos_accepted": true,
  "has_payment_method": true
}
`

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
	expectedResult    *BuyResult
	expectedError     string
}{
	{
		// successful buying
		suggestedCurrency: "EUR",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"0.99","currency":"EUR"}`,
		buyResponse:       mockOrderResponseJSON,
		expectedResult:    &BuyResult{State: "Complete"},
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
	cfg := Config{
		StoreBaseURL: mockServerURL,
	}
	sto := New(&cfg, authContext)

	buyOptions := &BuyOptions{
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
			case detailsPath("hello-world"):
				c.Assert(r.Method, Equals, "GET")
				w.Header().Set("Content-Type", "application/hal+json")
				w.Header().Set("X-Suggested-Currency", test.suggestedCurrency)
				w.WriteHeader(200)
				io.WriteString(w, MockDetailsJSON)
				searchServerCalled = true
			case ordersPath:
				c.Assert(r.Method, Equals, "GET")
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
				c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
				io.WriteString(w, `{"orders": []}`)
				purchaseServerGetCalled = true
			case buyPath:
				c.Assert(r.Method, Equals, "POST")
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Authorization"), Equals, s.expectedAuthorization(c, s.user))
				c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
				c.Check(r.Header.Get("Content-Type"), Equals, jsonContentType)
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
		cfg := Config{
			StoreBaseURL: mockServerURL,
		}
		sto := New(&cfg, authContext)

		// Find the snap first
		spec := SnapSpec{
			Name:     "hello-world",
			Channel:  "edge",
			Revision: snap.R(0),
		}
		snap, err := sto.SnapInfo(spec, s.user)
		c.Assert(snap, NotNil)
		c.Assert(err, IsNil)

		buyOptions := &BuyOptions{
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
	sto := New(&Config{}, nil)

	// no snap ID
	result, err := sto.Buy(&BuyOptions{
		Price:    1.0,
		Currency: "USD",
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: snap ID missing")

	// no price
	result, err = sto.Buy(&BuyOptions{
		SnapID:   "snap ID",
		Currency: "USD",
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: invalid expected price")

	// no currency
	result, err = sto.Buy(&BuyOptions{
		SnapID: "snap ID",
		Price:  1.0,
	}, s.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: currency missing")

	// no user
	result, err = sto.Buy(&BuyOptions{
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
				c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
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
		cfg := Config{
			StoreBaseURL: mockServerURL,
		}
		sto := New(&cfg, authContext)

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
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    url,
		ExtraHeaders: map[string]string{
			"Range": "bytes=5-",
		},
	}

	sto := New(&Config{}, nil)
	_, err = sto.doRequest(context.TODO(), sto.client, reqOptions, s.user)
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
	oldCache := s.store.cacher
	defer func() { s.store.cacher = oldCache }()
	obs := &cacheObserver{inCache: map[string]bool{"the-snaps-sha3_384": true}}
	s.store.cacher = obs

	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Fatalf("download should not be called when results come from the cache")
		return nil
	}

	snap := &snap.Info{}
	snap.Sha3_384 = "the-snaps-sha3_384"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)

	c.Check(obs.gets, DeepEquals, []string{fmt.Sprintf("%s:%s", snap.Sha3_384, path)})
	c.Check(obs.puts, IsNil)
}

func (s *storeTestSuite) TestDownloadCacheMiss(c *C) {
	oldCache := s.store.cacher
	defer func() { s.store.cacher = oldCache }()
	obs := &cacheObserver{inCache: map[string]bool{}}
	s.store.cacher = obs

	downloadWasCalled := false
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		downloadWasCalled = true
		return nil
	}

	snap := &snap.Info{}
	snap.Sha3_384 = "the-snaps-sha3_384"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := s.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	c.Check(downloadWasCalled, Equals, true)

	c.Check(obs.gets, DeepEquals, []string{fmt.Sprintf("the-snaps-sha3_384:%s", path)})
	c.Check(obs.puts, DeepEquals, []string{fmt.Sprintf("the-snaps-sha3_384:%s", path)})
}
