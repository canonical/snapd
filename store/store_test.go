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
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"
	"gopkg.in/retry.v1"

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
	"github.com/snapcore/snapd/testutil"
)

type remoteRepoTestSuite struct {
	testutil.BaseTest
	store     *Store
	logbuf    *bytes.Buffer
	user      *auth.UserState
	localUser *auth.UserState
	device    *auth.DeviceState

	origDownloadFunc func(context.Context, string, string, string, *auth.UserState, *Store, io.ReadWriteSeeker, int64, progress.Meter) error
	mockXDelta       *testutil.MockCmd
}

func TestStore(t *testing.T) { TestingT(t) }

var _ = Suite(&remoteRepoTestSuite{})

const (
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

	storeID string
}

func (ac *testAuthContext) Device() (*auth.DeviceState, error) {
	freshDevice := *ac.device
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

func (ac *testAuthContext) DeviceSessionRequest(nonce string) ([]byte, []byte, error) {
	serial, err := asserts.Decode([]byte(exSerial))
	if err != nil {
		return nil, nil, err
	}

	sessReq, err := asserts.Decode([]byte(strings.Replace(exDeviceSessionRequest, "@NONCE@", nonce, 1)))
	if err != nil {
		return nil, nil, err
	}

	return asserts.Encode(sessReq.(*asserts.DeviceSessionRequest)), asserts.Encode(serial.(*asserts.Serial)), nil
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

func (t *remoteRepoTestSuite) SetUpTest(c *C) {
	t.store = New(nil, nil)
	t.origDownloadFunc = download
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapMountDir, 0755), IsNil)

	t.logbuf = bytes.NewBuffer(nil)
	l, err := logger.NewConsoleLog(t.logbuf, logger.DefaultFlags)
	c.Assert(err, IsNil)
	logger.SetLogger(l)

	root, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	discharge, err := makeTestDischarge()
	c.Assert(err, IsNil)
	t.user, err = createTestUser(1, root, discharge)
	c.Assert(err, IsNil)
	t.localUser = &auth.UserState{
		ID:       11,
		Username: "test-user",
		Macaroon: "snapd-macaroon",
	}
	t.device = createTestDevice()
	t.mockXDelta = testutil.MockCommand(c, "xdelta3", "")

	MockDefaultRetryStrategy(&t.BaseTest, retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	)))
}

func (t *remoteRepoTestSuite) TearDownTest(c *C) {
	download = t.origDownloadFunc
	t.mockXDelta.Restore()
}

func (t *remoteRepoTestSuite) TearDownSuite(c *C) {
	logger.SimpleSetup()
}

func (t *remoteRepoTestSuite) expectedAuthorization(c *C, user *auth.UserState) string {
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

func (t *remoteRepoTestSuite) TestDownloadOK(c *C) {

	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Check(url, Equals, "anon-url")
		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := t.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I was downloaded")
}

func (t *remoteRepoTestSuite) TestDownloadRangeRequest(c *C) {
	partialContentStr := "partial content "

	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Check(resume, Equals, int64(len(partialContentStr)))
		c.Check(url, Equals, "anon-url")
		w.Write([]byte("was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = "abcdabcd"

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = t.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(targetFn)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, partialContentStr+"was downloaded")
}

func (t *remoteRepoTestSuite) TestDownloadRangeRequestRetryOnHashError(c *C) {
	partialContentStr := "partial content "

	n := 0
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		n++
		if n == 1 {
			// force sha3 error on first download
			c.Check(resume, Equals, int64(len(partialContentStr)))
			return HashError{"foo", "1234", "5678"}
		}
		w.Write([]byte("file was downloaded from scratch"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	snap.Sha3_384 = ""

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = t.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)

	content, err := ioutil.ReadFile(targetFn)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "file was downloaded from scratch")
}

func (t *remoteRepoTestSuite) TestDownloadRangeRequestFailOnHashError(c *C) {
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

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	err := ioutil.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644)
	c.Assert(err, IsNil)

	err = t.store.Download(context.TODO(), "foo", targetFn, &snap.DownloadInfo, nil, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `sha3-384 mismatch after patching "foo": got 1234 but expected 5678`)
	c.Assert(n, Equals, 2)
}

func (t *remoteRepoTestSuite) TestAuthenticatedDownloadDoesNotUseAnonURL(c *C) {
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		// check user is pass and auth url is used
		c.Check(user, Equals, t.user)
		c.Check(url, Equals, "AUTH-URL")

		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := t.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, t.user)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I was downloaded")
}

func (t *remoteRepoTestSuite) TestLocalUserDownloadUsesAnonURL(c *C) {
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		c.Check(url, Equals, "anon-url")

		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := t.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, t.localUser)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I was downloaded")
}

func (t *remoteRepoTestSuite) TestDownloadFails(c *C) {
	var tmpfile *os.File
	download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		return fmt.Errorf("uh, it failed")
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	// simulate a failed download
	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := t.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, ErrorMatches, "uh, it failed")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (t *remoteRepoTestSuite) TestDownloadSyncFails(c *C) {
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

	// simulate a failed sync
	path := filepath.Join(c.MkDir(), "downloaded-file")
	err := t.store.Download(context.TODO(), "foo", path, &snap.DownloadInfo, nil, nil)
	c.Assert(err, ErrorMatches, "fsync:.*")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (t *remoteRepoTestSuite) TestActualDownload(c *C) {
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

func (t *remoteRepoTestSuite) TestDownloadCancellation(c *C) {
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

func (t *remoteRepoTestSuite) TestActualDownloadNonPurchased401(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := New(&Config{}, nil)
	var buf bytes.Buffer
	err := download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, nopeSeeker{&buf}, -1, nil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot download non-free snap without purchase")
	c.Check(n, Equals, 1)
}

func (t *remoteRepoTestSuite) TestActualDownload404(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusNotFound)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := New(&Config{}, nil)
	var buf SillyBuffer
	err := download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &ErrDownload{})
	c.Check(err.(*ErrDownload).Code, Equals, http.StatusNotFound)
	c.Check(n, Equals, 1)
}

func (t *remoteRepoTestSuite) TestActualDownload500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := New(&Config{}, nil)
	var buf SillyBuffer
	err := download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &ErrDownload{})
	c.Check(err.(*ErrDownload).Code, Equals, http.StatusInternalServerError)
	c.Check(n, Equals, 5)
}

func (t *remoteRepoTestSuite) TestActualDownload500Once(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
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

func (t *remoteRepoTestSuite) TestActualDownloadResume(c *C) {
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

func (t *remoteRepoTestSuite) TestUseDeltas(c *C) {
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

func (t *remoteRepoTestSuite) TestDownloadWithDelta(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	for _, testCase := range deltaTests {
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
		err := t.store.Download(context.TODO(), "foo", path, &testCase.info, nil, nil)

		c.Assert(err, IsNil)
		defer os.Remove(path)
		content, err := ioutil.ReadFile(path)
		c.Assert(err, IsNil)
		c.Assert(string(content), Equals, testCase.expectedContent)
	}
}

var downloadDeltaTests = []struct {
	info          snap.DownloadInfo
	authenticated bool
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
	format:        "xdelta3",
	expectedURL:   "anon-delta-url",
	expectError:   false,
}, {
	// An authenticated request downloads the authenticated delta url.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "auth-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	authenticated: true,
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
	format:        "bsdiff",
	expectedURL:   "",
	expectError:   true,
}}

func (t *remoteRepoTestSuite) TestDownloadDelta(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	for _, testCase := range downloadDeltaTests {
		t.store.deltaFormat = testCase.format
		download = func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error {
			expectedUser := t.user
			if testCase.useLocalUser {
				expectedUser = t.localUser
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

		authedUser := t.user
		if testCase.useLocalUser {
			authedUser = t.localUser
		}
		if !testCase.authenticated {
			authedUser = nil
		}

		err = t.store.downloadDelta("snapname", &testCase.info, w, nil, authedUser)

		if testCase.expectError {
			c.Assert(err, NotNil)
		} else {
			c.Assert(err, IsNil)
			content, err := ioutil.ReadFile(w.Name())
			c.Assert(err, IsNil)
			c.Assert(string(content), Equals, "I was downloaded")
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

func (t *remoteRepoTestSuite) TestApplyDelta(c *C) {
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
			c.Assert(t.mockXDelta.Calls(), DeepEquals, [][]string{
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

func (t *remoteRepoTestSuite) TestDoRequestSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check user authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))
		// check device authorization is set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	repo := New(&Config{}, authContext)
	c.Assert(repo, NotNil)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := repo.doRequest(context.TODO(), repo.client, reqOptions, t.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (t *remoteRepoTestSuite) TestDoRequestDoesNotSetAuthForLocalOnlyUser(c *C) {
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

	authContext := &testAuthContext{c: c, device: t.device, user: t.localUser}
	repo := New(&Config{}, authContext)
	c.Assert(repo, NotNil)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := repo.doRequest(context.TODO(), repo.client, reqOptions, t.localUser)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (t *remoteRepoTestSuite) TestDoRequestAuthNoSerial(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check user authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))
		// check device authorization was not set
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		io.WriteString(w, "response-data")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// no serial and no device macaroon => no device auth
	t.device.Serial = ""
	t.device.SessionMacaroon = ""
	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	repo := New(&Config{}, authContext)
	c.Assert(repo, NotNil)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := repo.doRequest(context.TODO(), repo.client, reqOptions, t.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (t *remoteRepoTestSuite) TestDoRequestRefreshesAuth(c *C) {
	refresh, err := makeTestRefreshDischargeResponse()
	c.Assert(err, IsNil)
	c.Check(t.user.StoreDischarges[0], Not(Equals), refresh)

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
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))
		if t.user.StoreDischarges[0] == refresh {
			io.WriteString(w, "response-data")
		} else {
			w.Header().Set("WWW-Authenticate", "Macaroon needs_refresh=1")
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	repo := New(&Config{}, authContext)
	c.Assert(repo, NotNil)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := repo.doRequest(context.TODO(), repo.client, reqOptions, t.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
	c.Check(refreshDischargeEndpointHit, Equals, true)
}

func (t *remoteRepoTestSuite) TestDoRequestSetsAndRefreshesDeviceAuth(c *C) {
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
				w.WriteHeader(http.StatusUnauthorized)
			} else {
				c.Check(authorization, Equals, `Macaroon root="refreshed-session-macaroon"`)
				io.WriteString(w, "response-data")
			}
		case "/identity/api/v1/nonces":
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case "/identity/api/v1/sessions":
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

	MyAppsDeviceNonceAPI = mockServer.URL + "/identity/api/v1/nonces"
	MyAppsDeviceSessionAPI = mockServer.URL + "/identity/api/v1/sessions"

	// make sure device session is not set
	t.device.SessionMacaroon = ""
	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	repo := New(&Config{}, authContext)
	c.Assert(repo, NotNil)

	endpoint, _ := url.Parse(mockServer.URL)
	reqOptions := &requestOptions{Method: "GET", URL: endpoint}

	response, err := repo.doRequest(context.TODO(), repo.client, reqOptions, t.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
	c.Check(deviceSessionRequested, Equals, true)
	c.Check(refreshSessionRequested, Equals, true)
}

func (t *remoteRepoTestSuite) TestDoRequestSetsExtraHeaders(c *C) {
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

	repo := New(&Config{}, nil)
	c.Assert(repo, NotNil)
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

	response, err := repo.doRequest(context.TODO(), repo.client, reqOptions, t.user)
	defer response.Body.Close()
	c.Assert(err, IsNil)

	responseData, err := ioutil.ReadAll(response.Body)
	c.Assert(err, IsNil)
	c.Check(string(responseData), Equals, "response-data")
}

func (t *remoteRepoTestSuite) TestLoginUser(c *C) {
	macaroon, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	serializedMacaroon, err := auth.MacaroonSerialize(macaroon)
	c.Assert(err, IsNil)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, fmt.Sprintf(`{"macaroon": "%s"}`, serializedMacaroon))
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	MyAppsMacaroonACLAPI = mockServer.URL + "/acl/"

	discharge, err := makeTestDischarge()
	c.Assert(err, IsNil)
	serializedDischarge, err := auth.MacaroonSerialize(discharge)
	c.Assert(err, IsNil)
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

func (t *remoteRepoTestSuite) TestLoginUserMyAppsError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "{}")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	MyAppsMacaroonACLAPI = mockServer.URL + "/acl/"

	userMacaroon, userDischarge, err := LoginUser("username", "password", "otp")

	c.Assert(err, ErrorMatches, "cannot get snap access permission from store: .*")
	c.Check(userMacaroon, Equals, "")
	c.Check(userDischarge, Equals, "")
}

func (t *remoteRepoTestSuite) TestLoginUserSSOError(c *C) {
	macaroon, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	serializedMacaroon, err := auth.MacaroonSerialize(macaroon)
	c.Assert(err, IsNil)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, fmt.Sprintf(`{"macaroon": "%s"}`, serializedMacaroon))
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	MyAppsMacaroonACLAPI = mockServer.URL + "/acl/"

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

http --pretty=format --print b https://search.apps.ubuntu.com/api/v1/snaps/details/hello-world X-Ubuntu-Series:16 fields==anon_download_url,architecture,channel,download_sha3_384,summary,description,binary_filesize,download_url,icon_url,last_updated,package_name,prices,publisher,ratings_average,revision,screenshot_urls,snap_id,support_url,title,content,version,origin,developer_id,private,confinement channel==edge | xsel -b

on 2016-07-03. Then, by hand:
 * set prices to {"EUR": 0.99, "USD": 1.23}.
 * Screenshot URLS set manually.

On Ubuntu, apt install httpie xsel (although you could get http from
the http snap instead).

*/
const MockDetailsJSON = `{
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/snaps/details/hello-world?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement&channel=edge"
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
    "title": "hello-world",
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

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetails(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Path, Equals, "/details/hello-world")

		c.Check(r.URL.Query().Get("channel"), Equals, "edge")
		c.Check(r.URL.Query().Get("fields"), Equals, "abc,def")

		c.Check(r.Header.Get("X-Ubuntu-Series"), Equals, release.Series)
		c.Check(r.Header.Get("X-Ubuntu-Architecture"), Equals, arch.UbuntuArchitecture())
		c.Check(r.Header.Get("X-Ubuntu-Classic"), Equals, "false")

		c.Check(r.Header.Get("X-Ubuntu-Confinement"), Equals, "")

		w.Header().Set("X-Suggested-Currency", "GBP")
		w.WriteHeader(http.StatusOK)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI:   detailsURI,
		DetailFields: []string{"abc", "def"},
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
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
	c.Assert(result.Prices, DeepEquals, map[string]float64{"EUR": 0.99, "USD": 1.23})
	c.Assert(result.Screenshots, DeepEquals, []snap.ScreenshotInfo{
		{
			URL: "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/screenshot.png",
		},
	})
	c.Check(result.MustBuy, Equals, true)
	c.Check(result.Contact, Equals, "mailto:snappy-devel@lists.ubuntu.com")

	// Make sure the epoch (currently not sent by the store) defaults to "0"
	c.Check(result.Epoch, Equals, "0")

	c.Check(repo.SuggestedCurrency(), Equals, "GBP")

	// skip this one until the store supports it
	// c.Check(result.Private, Equals, true)

	c.Check(snap.Validate(result), IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetails500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI:   detailsURI,
		DetailFields: []string{},
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	_, err = repo.SnapInfo(spec, nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world" in channel "edge": got unexpected HTTP status code 500 via GET to "http://.*?/details/hello-world\?channel=edge"`)
	c.Assert(n, Equals, 5)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetails500once(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n > 1 {
			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, MockDetailsJSON)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI: detailsURI,
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Assert(n, Equals, 2)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsAndChannels(c *C) {
	// this test will break and should be melded into TestUbuntuStoreRepositoryDetails,
	// above, when the store provides the channels as part of details

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/details/hello-world")
			c.Check(r.URL.Query().Get("channel"), Equals, "")
			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(http.StatusOK)

			io.WriteString(w, MockDetailsJSON)
		case 1:
			c.Check(r.URL.Path, Equals, "/metadata")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{"_embedded":{"clickindex:package": [
{"channel": "stable",    "confinement": "strict",  "revision": 1, "version": "v1"},
{"channel": "candidate", "confinement": "strict",  "revision": 2, "version": "v2"},
{"channel": "beta",      "confinement": "devmode", "revision": 8, "version": "v8"},
{"channel": "edge",      "confinement": "devmode", "revision": 9, "version": "v9"}
]}}`)
		default:
			c.Fatalf("unexpected request to %q", r.URL.Path)
		}
		n++
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	bulkURI, err := url.Parse(mockServer.URL + "/metadata")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI: detailsURI,
		BulkURI:    bulkURI,
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Channels, DeepEquals, map[string]*snap.ChannelSnapInfo{
		"stable": {
			Revision:    snap.R(1),
			Version:     "v1",
			Confinement: snap.StrictConfinement,
			Channel:     "stable",
		},
		"candidate": {
			Revision:    snap.R(2),
			Version:     "v2",
			Confinement: snap.StrictConfinement,
			Channel:     "candidate",
		},
		"beta": {
			Revision:    snap.R(8),
			Version:     "v8",
			Confinement: snap.DevModeConfinement,
			Channel:     "beta",
		},
		"edge": {
			Revision:    snap.R(9),
			Version:     "v9",
			Confinement: snap.DevModeConfinement,
			Channel:     "edge",
		},
	})
	c.Check(snap.Validate(result), IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryNonDefaults(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	os.Setenv("SNAPPY_STORE_NO_CDN", "1")
	defer os.Unsetenv("SNAPPY_STORE_NO_CDN")

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "foo")

		c.Check(r.URL.Path, Equals, "/details/hello-world")

		c.Check(r.URL.Query().Get("channel"), Equals, "edge")

		c.Check(r.Header.Get("X-Ubuntu-Series"), Equals, "21")
		c.Check(r.Header.Get("X-Ubuntu-Architecture"), Equals, "archXYZ")
		c.Check(r.Header.Get("X-Ubuntu-Classic"), Equals, "true")
		c.Check(r.Header.Get("X-Ubuntu-No-CDN"), Equals, "true")

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := DefaultConfig()
	cfg.DetailsURI = detailsURI
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "foo"
	repo := New(cfg, nil)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryStoreIDFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "my-brand-store-id")

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := DefaultConfig()
	cfg.DetailsURI = detailsURI
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "fallback"
	repo := New(cfg, &testAuthContext{c: c, device: t.device, storeID: "my-brand-store-id"})
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryRevision(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, ordersPath) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		c.Check(r.URL.Path, Equals, "/details/hello-world")
		c.Check(r.URL.Query(), DeepEquals, url.Values{
			"channel":  []string{""},
			"revision": []string{"26"},
		})

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockDetailsJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)
	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := DefaultConfig()
	cfg.DetailsURI = detailsURI
	cfg.OrdersURI = ordersURI
	cfg.DetailFields = []string{}
	repo := New(cfg, nil)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(26),
	}
	result, err := repo.SnapInfo(spec, t.user)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Revision, DeepEquals, snap.R(27))
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsOopses(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/details/hello-world")
		c.Check(r.URL.Query().Get("channel"), Equals, "edge")

		w.Header().Set("X-Oops-Id", "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f")
		w.WriteHeader(http.StatusInternalServerError)

		io.WriteString(w, `{"oops": "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI: detailsURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	_, err = repo.SnapInfo(spec, nil)
	c.Assert(err, ErrorMatches, `cannot get details for snap "hello-world" in channel "edge": got unexpected HTTP status code 5.. via GET to "http://\S+" \[OOPS-[[:xdigit:]]*\]`)
}

/*
acquired via

http --pretty=format --print b https://search.apps.ubuntu.com/api/v1/snaps/details/no:such:package X-Ubuntu-Series:16 fields==anon_download_url,architecture,channel,download_sha512,summary,description,binary_filesize,download_url,icon_url,last_updated,package_name,prices,publisher,ratings_average,revision,snap_id,support_url,title,content,version,origin,developer_id,private,confinement channel==edge | xsel -b

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

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryNoDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/details/no-such-pkg")

		q := r.URL.Query()
		c.Check(q.Get("channel"), Equals, "edge")
		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI: detailsURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	// the actual test
	spec := SnapSpec{
		Name:     "no-such-pkg",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
	c.Assert(err, NotNil)
	c.Assert(result, IsNil)
}

func (t *remoteRepoTestSuite) TestStructFields(c *C) {
	type s struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(getStructFields(s{}), DeepEquals, []string{"hello", "potato"})
}

/* acquired via:
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://search.apps.ubuntu.com/api/v1/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&q=hello' | python -m json.tool | xsel -b
Screenshot URLS set manually.
*/
const MockSearchJSON = `{
    "_embedded": {
        "clickindex:package": [
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
                    }
                },
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
                "title": "hello-world",
                "version": "6.0"
            }
        ]
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "first": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "last": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        }
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreFindQueries(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")
		section := query.Get("section")

		c.Check(r.URL.Path, Equals, "/search")
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

	serverURL, _ := url.Parse(mockServer.URL)
	searchURI, _ := serverURL.Parse("/search")
	detailsURI, _ := serverURL.Parse("/details/")
	cfg := Config{
		DetailsURI:   detailsURI,
		SearchURI:    searchURI,
		DetailFields: []string{"abc", "def"},
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	for _, query := range []Search{
		{Query: "hello", Prefix: true},
		{Query: "hello"},
		{Section: "db"},
		{Query: "hello", Section: "db"},
	} {
		repo.Find(&query, nil)
	}
}

/* acquired via:
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://search.apps.ubuntu.com/api/v1/snaps/sections'
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
    "curies": [
      {
        "href": "https://search.apps.ubuntu.com/docs/#reltype-{rel}", 
        "name": "clickindex", 
        "templated": true, 
        "type": "text/html"
      }
    ], 
    "self": {
      "href": "http://search.apps.ubuntu.com/api/v1/snaps/sections"
    }
  }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreSectionsQuery(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/snaps/sections")
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockSectionsJSON)
		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	searchSectionsURI, _ := serverURL.Parse("/snaps/sections")
	cfg := Config{
		SectionsURI: searchSectionsURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	sections, err := repo.Sections(t.user)
	c.Check(err, IsNil)
	c.Check(sections, DeepEquals, []string{"featured", "database"})
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindPrivate(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")

		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/search")
			c.Check(name, Equals, "")
			c.Check(q, Equals, "foo")
			c.Check(query.Get("private"), Equals, "true")
		default:
			c.Fatalf("what? %d", n)
		}

		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, strings.Replace(MockSearchJSON, `"EUR": 2.99, "USD": 3.49`, "", -1))

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	searchURI, _ := serverURL.Parse("/search")
	cfg := Config{
		SearchURI: searchURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	_, err := repo.Find(&Search{Query: "foo", Private: true}, t.user)
	c.Check(err, IsNil)

	_, err = repo.Find(&Search{Query: "foo", Private: true}, nil)
	c.Check(err, Equals, ErrUnauthenticated)

	_, err = repo.Find(&Search{Query: "name:foo", Private: true}, t.user)
	c.Check(err, Equals, ErrBadQuery)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindFailures(c *C) {
	repo := New(&Config{SearchURI: new(url.URL)}, nil)
	_, err := repo.Find(&Search{Query: "foo:bar"}, nil)
	c.Check(err, Equals, ErrBadQuery)
	_, err = repo.Find(&Search{Query: "foo", Private: true, Prefix: true}, t.user)
	c.Check(err, Equals, ErrBadQuery)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindFails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		http.Error(w, http.StatusText(http.StatusTeapot), http.StatusTeapot)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := Config{
		SearchURI:    searchURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find(&Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `cannot search: got unexpected HTTP status code 418 via GET to "http://\S+[?&]q=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindBadContentType(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Query().Get("q"), Equals, "hello")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := Config{
		SearchURI:    searchURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find(&Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `received an unexpected content type \("text/plain[^"]+"\) when trying to search via "http://\S+[?&]q=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindBadBody(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		c.Check(query.Get("q"), Equals, "hello")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, "<hello>")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := Config{
		SearchURI:    searchURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find(&Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `invalid character '<' looking for beginning of value`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFind500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := Config{
		SearchURI:    searchURI,
		DetailFields: []string{},
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	_, err = repo.Find(&Search{Query: "hello"}, nil)
	c.Check(err, ErrorMatches, `cannot search: got unexpected HTTP status code 500 via GET to "http://\S+[?&]q=hello.*"`)
	c.Assert(n, Equals, 5)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFind500once(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.Header().Set("Content-Type", "application/hal+json")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, strings.Replace(MockSearchJSON, `"EUR": 2.99, "USD": 3.49`, "", -1))
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := Config{
		SearchURI:    searchURI,
		DetailFields: []string{},
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find(&Search{Query: "hello"}, nil)
	c.Check(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Assert(n, Equals, 2)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindAuthFailed(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))

		query := r.URL.Query()
		c.Check(query.Get("q"), Equals, "foo")
		if release.OnClassic {
			c.Check(query.Get("confinement"), Matches, `strict,classic|classic,strict`)
		} else {
			c.Check(query.Get("confinement"), Equals, "strict")
		}
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "{}")
	}))
	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)
	cfg := Config{
		SearchURI:    searchURI,
		OrdersURI:    ordersURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find(&Search{Query: "foo"}, t.user)
	c.Assert(err, IsNil)

	// Check that we log an error.
	c.Check(t.logbuf.String(), Matches, "(?ms).* cannot get user orders: invalid credentials")

	// But still successfully return snap information.
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].SnapID, Equals, helloWorldSnapID)
	c.Check(snaps[0].Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Check(snaps[0].MustBuy, Equals, true)
}

/* acquired via:
(against production "hello-world")
$ curl -s --data-binary '{"snaps":[{"snap_id":"buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ","channel":"stable","revision":25,"epoch":"0","confinement":"strict"}],"fields":["anon_download_url","architecture","channel","download_sha512","summary","description","binary_filesize","download_url","icon_url","last_updated","package_name","prices","publisher","ratings_average","revision","snap_id","support_url","title","content","version","origin","developer_id","private","confinement"]}'  -H 'content-type: application/json' -H 'X-Ubuntu-Release: 16' -H 'X-Ubuntu-Wire-Protocol: 1' -H "accept: application/hal+json" https://search.apps.ubuntu.com/api/v1/snaps/metadata | python3 -m json.tool --sort-keys | xsel -b
*/
var MockUpdatesJSON = `
{
    "_embedded": {
        "clickindex:package": [
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
                    }
                },
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
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 26,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "hello-world",
                "version": "6.1"
            }
        ]
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ]
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefresh(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(1),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].PublisherID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshUnauthorised(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}

	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	_, err = repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(24),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(n, Equals, 1)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 401 via POST to "http://.*?/updates/"`)
}
func (t *remoteRepoTestSuite) TestListRefresh500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	_, err = repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(24),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 500 via POST to "http://.*?/updates/"`)
	c.Assert(n, Equals, 5)
}

func (t *remoteRepoTestSuite) TestListRefresh500DurationExceeded(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		time.Sleep(time.Duration(2) * time.Second)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	_, err = repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(24),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 500 via POST to "http://.*?/updates/"`)
	c.Assert(n, Equals, 1)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshSkipCurrent(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(26),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshSkipBlocked(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(25),
			Epoch:    "0",
			Block:    []snap.Revision{snap.R(26)},
		},
	}, nil)
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
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
                    }
                },
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
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 26,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "hello-world",
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
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ]
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDefaultsDeltasOnClassicOnly(c *C) {
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
			c.Check(r.Header.Get("X-Ubuntu-Delta-Formats"), Equals, t.deltaFormatStr)
		}))
		defer mockServer.Close()

		var err error
		bulkURI, err := url.Parse(mockServer.URL + "/updates/")
		c.Assert(err, IsNil)
		cfg := Config{
			BulkURI: bulkURI,
		}
		repo := New(&cfg, nil)
		c.Assert(repo, NotNil)

		repo.ListRefresh([]*RefreshCandidate{
			{
				SnapID:   helloWorldSnapID,
				Channel:  "stable",
				Revision: snap.R(24),
				Epoch:    "0",
			},
		}, nil)
	}
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshWithDeltas(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		c.Assert(resp.Fields, DeepEquals, getStructFields(snapDetails{}))

		io.WriteString(w, MockUpdatesWithDeltasJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(24),
			Epoch:    "0",
		},
	}, nil)
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

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshWithoutDeltas(c *C) {
	// Verify the X-Delta-Format header is not set.
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "0"), IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(24),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryUpdateNotSendLocalRevs(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			"epoch":       "0",
			"confinement": "",
		})

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := Config{
		BulkURI: bulkURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	_, err = repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(-2),
			Epoch:    "0",
		},
	}, nil)
	c.Assert(err, IsNil)
}

func (t *remoteRepoTestSuite) TestStructFieldsSurvivesNoTag(c *C) {
	type s struct {
		Foo int `json:"hello"`
		Bar int
	}
	c.Assert(getStructFields(s{}), DeepEquals, []string{"hello"})
}

func (t *remoteRepoTestSuite) TestCpiURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := cpiURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := cpiURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestAuthLocationDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := authLocation()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := authLocation()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestAuthURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := authURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := authURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestAssertsURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := assertsURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := assertsURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestMyAppsURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", ""), IsNil)
	before := myappsURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_STORE", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_STORE", "")
	after := myappsURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestDefaultConfig(c *C) {
	c.Check(strings.HasPrefix(defaultConfig.SearchURI.String(), "https://search.apps.ubuntu.com/api/v1/snaps/search"), Equals, true)
	c.Check(strings.HasPrefix(defaultConfig.BulkURI.String(), "https://search.apps.ubuntu.com/api/v1/snaps/metadata"), Equals, true)
	c.Check(defaultConfig.AssertionsURI.String(), Equals, "https://assertions.ubuntu.com/v1/assertions/")
}

func (t *remoteRepoTestSuite) TestNew(c *C) {
	aStore := New(nil, nil)
	fields := strings.Join(detailFields, ",")
	// check for fields
	c.Check(aStore.detailFields, DeepEquals, detailFields)
	c.Check(aStore.searchURI.Query().Get("fields"), Equals, fields)
	c.Check(aStore.detailsURI.Query().Get("fields"), Equals, fields)
	c.Check(aStore.bulkURI.Query(), DeepEquals, url.Values{})
	c.Check(aStore.sectionsURI.Query(), DeepEquals, url.Values{})
	c.Check(aStore.assertionsURI.Query(), DeepEquals, url.Values{})
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

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryAssertion(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/assertions/snap-declaration/16/snapidfoo")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	assertionsURI, err := url.Parse(mockServer.URL + "/assertions/")
	c.Assert(err, IsNil)

	cfg := Config{
		AssertionsURI: assertionsURI,
	}
	authContext := &testAuthContext{c: c, device: t.device}
	repo := New(&cfg, authContext)

	a, err := repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryAssertionNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/assertions/snap-declaration/16/snapidfoo")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		io.WriteString(w, `{"status": 404,"title": "not found"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	assertionsURI, err := url.Parse(mockServer.URL + "/assertions/")
	c.Assert(err, IsNil)
	cfg := Config{
		AssertionsURI: assertionsURI,
	}
	repo := New(&cfg, nil)

	_, err = repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Check(err, DeepEquals, &AssertionNotFoundError{
		Ref: &asserts.Ref{
			Type:       asserts.SnapDeclarationType,
			PrimaryKey: []string{"16", "snapidfoo"},
		},
	})
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryAssertion500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	assertionsURI, err := url.Parse(mockServer.URL + "/assertions/")
	c.Assert(err, IsNil)
	cfg := Config{
		AssertionsURI: assertionsURI,
	}
	repo := New(&cfg, nil)

	_, err = repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, ErrorMatches, `cannot fetch assertion: got unexpected HTTP status code 500 via .+`)
	c.Assert(n, Equals, 5)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositorySuggestedCurrency(c *C) {
	suggestedCurrency := "GBP"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Suggested-Currency", suggestedCurrency)
		w.WriteHeader(http.StatusOK)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := Config{
		DetailsURI: detailsURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	// the store doesn't know the currency until after the first search, so fall back to dollars
	c.Check(repo.SuggestedCurrency(), Equals, "USD")

	// we should soon have a suggested currency
	spec := SnapSpec{
		Name:     "hello-world",
		Channel:  "edge",
		Revision: snap.R(0),
	}
	result, err := repo.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(repo.SuggestedCurrency(), Equals, "GBP")

	suggestedCurrency = "EUR"

	// checking the currency updates
	result, err = repo.SnapInfo(spec, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(repo.SuggestedCurrency(), Equals, "EUR")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrders(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockOrdersJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	cfg := Config{
		OrdersURI: ordersURI,
	}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err = repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, IsNil)

	c.Check(helloWorld.MustBuy, Equals, false)
	c.Check(funkyApp.MustBuy, Equals, false)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersFailedAccess(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)
	cfg := Config{
		OrdersURI: ordersURI,
	}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err = repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, NotNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersNoAuth(c *C) {
	cfg := Config{}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err := repo.decorateOrders(snaps, "edge", nil)
	c.Assert(err, IsNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersAllFree(c *C) {
	requestRecieved := false

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		requestRecieved = true
		io.WriteString(w, `{"orders": []}`)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)
	cfg := Config{
		OrdersURI: ordersURI,
	}

	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	// This snap is free
	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	// This snap is also free
	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID

	snaps := []*snap.Info{helloWorld, funkyApp}

	// There should be no request to the purchase server.
	err = repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, IsNil)
	c.Check(requestRecieved, Equals, false)
}

const ordersPath = "/purchases/v1/orders"
const customersMePath = "/purchases/v1/customers/me"

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersSingle(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		io.WriteString(w, mockSingleOrderJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	cfg := Config{
		OrdersURI: ordersURI,
	}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	snaps := []*snap.Info{helloWorld}

	err = repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersSingleFreeSnap(c *C) {
	cfg := Config{}
	repo := New(&cfg, nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	snaps := []*snap.Info{helloWorld}

	err := repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersSingleNotFound(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	cfg := Config{
		OrdersURI: ordersURI,
	}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	snaps := []*snap.Info{helloWorld}

	err = repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecorateOrdersTokenExpired(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
		c.Check(r.URL.Path, Equals, ordersPath)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	cfg := Config{
		OrdersURI: ordersURI,
	}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	snaps := []*snap.Info{helloWorld}

	err = repo.decorateOrders(snaps, "edge", t.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreMustBuy(c *C) {
	free := map[string]float64{}
	priced := map[string]float64{"USD": 2.99}

	// Never need to buy a free snap.
	c.Check(mustBuy(free, true), Equals, false)
	c.Check(mustBuy(free, false), Equals, false)

	// Don't need to buy snaps that have been bought.
	c.Check(mustBuy(priced, true), Equals, false)

	// Need to buy snaps that aren't bought.
	c.Check(mustBuy(priced, false), Equals, true)
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
		buyStatus:         http.StatusBadRequest,
		buyErrorCode:      "invalid-field",
		buyErrorMessage:   "invalid price specified",
		price:             5.99,
		expectedError:     "cannot buy snap: bad request: store reported an error: invalid price specified",
	},
	{
		// failure due to unknown snap ID
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"invalid snap ID","amount":"0.99","currency":"EUR"}`,
		buyStatus:         http.StatusNotFound,
		buyErrorCode:      "not-found",
		buyErrorMessage:   "Not found",
		snapID:            "invalid snap ID",
		price:             0.99,
		currency:          "EUR",
		expectedError:     "cannot buy snap: server says not found (snap got removed?)",
	},
	{
		// failure due to "Purchase failed"
		suggestedCurrency: "USD",
		expectedInput:     `{"snap_id":"` + helloWorldSnapID + `","amount":"1.23","currency":"USD"}`,
		buyStatus:         http.StatusPaymentRequired,
		buyErrorCode:      "request-failed",
		buyErrorMessage:   "Purchase failed",
		expectedError:     "payment declined",
	},
}

func (t *remoteRepoTestSuite) TestUbuntuStoreBuy500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))

	detailsURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
	c.Assert(err, IsNil)
	customersMeURI, err := url.Parse(mockPurchasesServer.URL + customersMePath)
	c.Assert(err, IsNil)

	authContext := &testAuthContext{c: c, device: t.device, user: t.user}
	cfg := Config{
		CustomersMeURI: customersMeURI,
		DetailsURI:     detailsURI,
		OrdersURI:      ordersURI,
	}
	repo := New(&cfg, authContext)
	c.Assert(repo, NotNil)

	buyOptions := &BuyOptions{
		SnapID:   helloWorldSnapID,
		Currency: "USD",
		Price:    1,
	}
	_, err = repo.Buy(buyOptions, t.user)
	c.Assert(err, NotNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreBuy(c *C) {
	for _, test := range buyTests {
		searchServerCalled := false
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/hello-world")
			w.Header().Set("Content-Type", "application/hal+json")
			w.Header().Set("X-Suggested-Currency", test.suggestedCurrency)
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, MockDetailsJSON)
			searchServerCalled = true
		}))
		c.Assert(mockServer, NotNil)
		defer mockServer.Close()

		purchaseServerGetCalled := false
		purchaseServerPostCalled := false
		mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
				c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
				switch r.URL.Path {
				case ordersPath:
					io.WriteString(w, `{"orders": []}`)
				case customersMePath:
					io.WriteString(w, customersMeValid)
				default:
					c.Fail()
				}
				purchaseServerGetCalled = true
			case "POST":
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
				c.Check(r.Header.Get("Accept"), Equals, jsonContentType)
				c.Check(r.Header.Get("Content-Type"), Equals, jsonContentType)
				c.Check(r.URL.Path, Equals, ordersPath)
				jsonReq, err := ioutil.ReadAll(r.Body)
				c.Assert(err, IsNil)
				c.Check(string(jsonReq), Equals, test.expectedInput)
				if test.buyErrorCode == "" {
					io.WriteString(w, test.buyResponse)
				} else {
					w.WriteHeader(test.buyStatus)
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
				c.Error("Unexpected request method: ", r.Method)
			}
		}))

		c.Assert(mockPurchasesServer, NotNil)
		defer mockPurchasesServer.Close()

		detailsURI, err := url.Parse(mockServer.URL)
		c.Assert(err, IsNil)
		ordersURI, err := url.Parse(mockPurchasesServer.URL + ordersPath)
		c.Assert(err, IsNil)
		customersMeURI, err := url.Parse(mockPurchasesServer.URL + customersMePath)
		c.Assert(err, IsNil)

		authContext := &testAuthContext{c: c, device: t.device, user: t.user}
		cfg := Config{
			CustomersMeURI: customersMeURI,
			DetailsURI:     detailsURI,
			OrdersURI:      ordersURI,
		}
		repo := New(&cfg, authContext)
		c.Assert(repo, NotNil)

		// Find the snap first
		spec := SnapSpec{
			Name:     "hello-world",
			Channel:  "edge",
			Revision: snap.R(0),
		}
		snap, err := repo.SnapInfo(spec, t.user)
		c.Assert(snap, NotNil)
		c.Assert(err, IsNil)

		buyOptions := &BuyOptions{
			SnapID:   snap.SnapID,
			Currency: repo.SuggestedCurrency(),
			Price:    snap.Prices[repo.SuggestedCurrency()],
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
		result, err := repo.Buy(buyOptions, t.user)

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

func (t *remoteRepoTestSuite) TestUbuntuStoreBuyFailArgumentChecking(c *C) {
	repo := New(&Config{}, nil)
	c.Assert(repo, NotNil)

	// no snap ID
	result, err := repo.Buy(&BuyOptions{
		Price:    1.0,
		Currency: "USD",
	}, t.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: snap ID missing")

	// no price
	result, err = repo.Buy(&BuyOptions{
		SnapID:   "snap ID",
		Currency: "USD",
	}, t.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: invalid expected price")

	// no currency
	result, err = repo.Buy(&BuyOptions{
		SnapID: "snap ID",
		Price:  1.0,
	}, t.user)
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap: currency missing")

	// no user
	result, err = repo.Buy(&BuyOptions{
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
			w.WriteHeader(http.StatusNotFound)
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
			w.WriteHeader(http.StatusInternalServerError)
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
			c.Check(err.Error(), Equals, `store reported an error: message 1`)
		},
		NumOfCalls: 5,
	},
}

func (t *remoteRepoTestSuite) TestUbuntuStoreReadyToBuy(c *C) {
	for _, test := range readyToBuyTests {
		purchaseServerGetCalled := 0
		mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				// check device authorization is set, implicitly checking doRequest was used
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
				c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
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

		customersMeURI, err := url.Parse(mockPurchasesServer.URL + customersMePath)
		c.Assert(err, IsNil)

		authContext := &testAuthContext{c: c, device: t.device, user: t.user}
		cfg := Config{
			CustomersMeURI: customersMeURI,
		}
		repo := New(&cfg, authContext)
		c.Assert(repo, NotNil)

		err = repo.ReadyToBuy(t.user)
		test.Test(c, err)
		c.Check(purchaseServerGetCalled, Equals, test.NumOfCalls)
	}
}

func (t *remoteRepoTestSuite) TestDoRequestSetRangeHeaderOnRedirect(c *C) {
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
	_, err = sto.doRequest(context.TODO(), sto.client, reqOptions, t.user)
	c.Assert(err, IsNil)
}
