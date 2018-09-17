// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/juju/ratelimit"
	"golang.org/x/net/context"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type downloadSuite struct {
	testutil.BaseTest
}

var _ = Suite(&downloadSuite{})

func (s *downloadSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	mockXdelta := testutil.MockCommand(c, "xdelta3", "")
	s.AddCleanup(mockXdelta.Restore)
}

func (s *downloadSuite) TestActualDownload(c *C) {
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

func (s *downloadSuite) TestActualDownloadNoCDN(c *C) {
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

func (s *downloadSuite) TestActualDownloadFullCloudInfoFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, `cloud-name="aws" region="us-east-1" availability-zone="us-east-1c"`)

		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	device := createTestDevice()
	theStore := store.New(&store.Config{}, &testAuthContext{c: c, device: device, cloudInfo: &auth.CloudInfo{Name: "aws", Region: "us-east-1", AvailabilityZone: "us-east-1c"}})

	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
}

func (s *downloadSuite) TestActualDownloadLessDetailedCloudInfoFromAuthContext(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, `cloud-name="openstack" availability-zone="nova"`)

		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	device := createTestDevice()
	theStore := store.New(&store.Config{}, &testAuthContext{c: c, device: device, cloudInfo: &auth.CloudInfo{Name: "openstack", Region: "", AvailabilityZone: "nova"}})

	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
}

func (s *downloadSuite) TestDownloadCancellation(c *C) {
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

type nopeSeeker struct{ io.ReadWriter }

func (nopeSeeker) Seek(int64, int) (int64, error) {
	return -1, errors.New("what is this, quidditch?")
}

func (s *downloadSuite) TestActualDownloadNonPurchased402(c *C) {
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

func (s *downloadSuite) TestActualDownload404(c *C) {
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

func (s *downloadSuite) TestActualDownload500(c *C) {
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

func (s *downloadSuite) TestActualDownload500Once(c *C) {
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

func (s *downloadSuite) TestActualDownloadResume(c *C) {
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

func (s *downloadSuite) TestUseDeltas(c *C) {
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

func (s *downloadSuite) TestDownloadWithDelta(c *C) {
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

		theStore := store.New(&store.Config{}, nil)
		path := filepath.Join(c.MkDir(), "subdir", "downloaded-file")
		err := theStore.Download(context.TODO(), "foo", path, &testCase.info, nil, nil, nil)

		c.Assert(err, IsNil)
		defer os.Remove(path)
		c.Assert(path, testutil.FileEquals, testCase.expectedContent)
	}
}

func (s *downloadSuite) TestActualDownloadRateLimited(c *C) {
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

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	err := store.Download(context.TODO(), "example-name", "", ts.URL, nil, theStore, &buf, 0, nil, &store.DownloadOptions{RateLimit: 1})
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, canary)
	c.Check(ratelimitReaderUsed, Equals, true)
}
