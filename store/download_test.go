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
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/juju/ratelimit"
	. "gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type downloadSuite struct {
	mockXdelta *testutil.MockCmd

	testutil.BaseTest
}

var _ = Suite(&downloadSuite{})

func (s *downloadSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	store.MockDownloadRetryStrategy(&s.BaseTest, retry.LimitCount(5, retry.Exponential{
		Initial: time.Millisecond,
		Factor:  2.5,
	}))

	s.mockXdelta = testutil.MockCommand(c, "xdelta3", "")
	s.AddCleanup(s.mockXdelta.Restore)
}

func (s *downloadSuite) TestActualDownload(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, "")
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, "")
		c.Check(r.Header.Get("Snap-Refresh-Reason"), Equals, "")
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

func (s *downloadSuite) TestActualDownloadAutoRefresh(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-Refresh-Reason"), Equals, "scheduled")
		n++
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, &store.DownloadOptions{Scheduled: true})
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestActualDownloadNoCDN(c *C) {
	os.Setenv("SNAPPY_STORE_NO_CDN", "1")
	defer os.Unsetenv("SNAPPY_STORE_NO_CDN")

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, "none")
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, "")
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
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, `cloud-name="aws" region="us-east-1" availability-zone="us-east-1c"`)

		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	device := createTestDevice()
	theStore := store.New(&store.Config{}, &testDauthContext{c: c, device: device, cloudInfo: &auth.CloudInfo{Name: "aws", Region: "us-east-1", AvailabilityZone: "us-east-1c"}})

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
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, `cloud-name="openstack" availability-zone="nova"`)

		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	device := createTestDevice()
	theStore := store.New(&store.Config{}, &testDauthContext{c: c, device: device, cloudInfo: &auth.CloudInfo{Name: "openstack", Region: "", AvailabilityZone: "nova"}})

	var buf SillyBuffer
	// keep tests happy
	sha3 := ""
	err := store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
}

func (s *downloadSuite) TestDownloadCancellation(c *C) {
	ctx, cancel := context.WithCancel(context.Background())

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "foo")
		cancel()
		io.WriteString(w, "bar")
		time.Sleep(10 * time.Millisecond)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)

	sha3 := ""
	var buf SillyBuffer
	err := store.Download(ctx, "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil)

	c.Check(n, Equals, 1)
	c.Assert(err, ErrorMatches, "the download has been cancelled: context canceled")
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
	c.Check(err.Error(), Equals, "please buy foo before installing it")
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

// SillyBuffer is a ReadWriteSeekTruncater buffer with a limited size for the tests
// (bytes does not implement an ReadWriteSeekTruncater)
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
func (sb *SillyBuffer) Truncate(size int64) error {
	if size < 0 || size > int64(len(sb.buf)) {
		return fmt.Errorf("truncate out of bounds: %d", size)
	}
	sb.end = size
	return nil
}
func (sb *SillyBuffer) String() string {
	return string(sb.buf[0:sb.pos])
}

func (s *downloadSuite) TestActualDownloadResume(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(206)
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

func (s *downloadSuite) TestActualDownloadServerNoResumeHandeled(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++

		switch n {
		case 1:
			c.Check(r.Header["Range"], HasLen, 1)
		default:
			c.Fatal("only one request expected")
		}
		// server does not do partial content and sends full data instead
		w.WriteHeader(200)
		io.WriteString(w, "some data")
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
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	restore := release.MockOnClassic(false)
	defer restore()

	scenarios := []struct {
		env       string
		classic   bool
		wantDelta bool
	}{
		{env: "", classic: false, wantDelta: true},
		{env: "", classic: true, wantDelta: true},

		{env: "0", classic: false, wantDelta: false},
		{env: "0", classic: true, wantDelta: false},

		{env: "1", classic: false, wantDelta: true},
		{env: "1", classic: true, wantDelta: true},
	}

	for _, scenario := range scenarios {
		comment := Commentf("%#v", scenario)

		// setup the env var for the scenario
		os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", scenario.env)
		release.MockOnClassic(scenario.classic)

		// run the check for delta usage
		c.Check(len(squashfs.SupportedDeltaFormats(
			squashfs.DeltaFormatOpts{WithSnapDeltaFormat: true})) > 0, Equals, scenario.wantDelta, comment)

		if !scenario.wantDelta {
			// if we didn't want deltas, the scenario should have
			// disabled via an env var
			c.Assert(scenario.env == "0", Equals, true)
		}
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
		DownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "delta-url", Format: "xdelta3"},
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
		DownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "delta-url", Format: "xdelta3"},
		},
	},
	expectedContent: "full-snap-url-content",
}, {
	// Use first delta when more than one is reported for the same format
	downloads: downloadBehaviour{
		{url: "delta-url"},
	},
	info: snap.DownloadInfo{
		DownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "delta-url", Format: "xdelta3"},
			{DownloadURL: "delta-url-2", Format: "xdelta3"},
		},
	},
	expectedContent: "snap-content-via-delta",
}}

func (s *downloadSuite) TestDownloadWithDelta(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	for i, testCase := range deltaTests {
		c.Log("tc:", i)
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
		restore = store.MockApplyDelta(func(_ context.Context, _ *store.Store, name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
			c.Check(*deltaInfo, Equals, testCase.info.Deltas[0])
			err := os.WriteFile(targetPath, []byte("snap-content-via-delta"), 0644)
			c.Assert(err, IsNil)
			return nil
		})
		defer restore()

		theStore := store.New(&store.Config{}, nil)
		squasgfsRestore := store.MockSquashfsApplyDelta(func(ctx context.Context, sourceSnap, deltaFile, targetSnap string) error {
			return nil
		})
		defer squasgfsRestore()

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

func (s *downloadSuite) TestActualDownloadIcon(c *C) {
	n := 0
	const existingEtag = ""
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Snap-CDN"), Equals, "")
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, "")
		c.Check(r.Header.Get("Snap-Refresh-Reason"), Equals, "")
		n++
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	newEtag, err := store.DownloadIconImpl(context.TODO(), "foo", existingEtag, mockServer.URL, theStore, &buf)
	c.Assert(err, IsNil)
	c.Check(newEtag, Equals, "")
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestActualDownloadIconWithNewEtag(c *C) {
	s.testActualDownloadIconWithNewEtagVariant(c, "etag")
	s.testActualDownloadIconWithNewEtagVariant(c, "Etag")
	s.testActualDownloadIconWithNewEtagVariant(c, "ETag")
}

func (s *downloadSuite) testActualDownloadIconWithNewEtagVariant(c *C, etagSpelling string) {
	n := 0
	const existingEtag = ""
	const newEtag = "some-unique-value"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Assert(r.Header.Get("If-None-Match"), Equals, existingEtag)

		w.Header().Set(etagSpelling, newEtag) // set the http header according to etagSpelling
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", existingEtag, mockServer.URL, theStore, &buf)
	c.Assert(err, IsNil)
	c.Check(receivedEtag, Equals, newEtag)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestActualDownloadIconWithExistingEtag(c *C) {
	n := 0
	const etag = "some-unique-value"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Assert(r.Header.Get("If-None-Match"), Equals, etag)

		w.Header().Set("Etag", etag) // use correct etag case here
		w.WriteHeader(304)           // 304 Not Modified
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", etag, mockServer.URL, theStore, &buf)
	c.Check(err, Equals, store.ErrIconUnchanged)
	c.Check(receivedEtag, Equals, "") // since we return an error, expect empty etag
	c.Check(buf.String(), Equals, "")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestActualDownloadIconWithChangedEtag(c *C) {
	n := 0
	const existingEtag = "some-unique-value"
	const newEtag = "another-unique-value"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Assert(r.Header.Get("If-None-Match"), Equals, existingEtag)

		w.Header().Set("ETag", newEtag) // use another etag variant here
		// return 200, not 304, since etag is different
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", existingEtag, mockServer.URL, theStore, &buf)
	c.Assert(err, IsNil)
	c.Check(receivedEtag, Equals, newEtag)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestActualDownloadIconTooLarge(c *C) {
	var maxSize int64 = 1000 // Must be less than size of SillyBuffer, so we can write enough to exceed the limit
	restore := store.MockMaxIconFilesize(maxSize)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		response := make([]byte, maxSize+1)
		w.Write(response)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", "fake-etag", mockServer.URL, theStore, &buf)
	c.Assert(err, ErrorMatches, "unsupported Content-Length .*")
	c.Check(receivedEtag, Equals, "")
	c.Check(n, Equals, 1)
}

type BadWriter struct {
	SillyBuffer
}

func (bw *BadWriter) Write(p []byte) (n int, err error) {
	// Do the write
	bw.SillyBuffer.Write(p)
	// but return EOF
	return -1, io.EOF
}

func (s *downloadSuite) TestActualDownloadIconCopyError(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		response := make([]byte, 5)
		for i := range response[:5] {
			// respond with 5 'a' bytes so we can check that it's been seeked and truncated
			response[i] = 'a'
		}
		w.Write(response)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf BadWriter
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", "fake-etag", mockServer.URL, theStore, &buf)
	c.Check(err, testutil.ErrorIs, io.EOF)
	c.Check(receivedEtag, Equals, "")
	c.Check(n, Equals, 5)
	// Check that the buffer only has 5 'a' bytes, indicating that it was
	// seeked/truncated after each failed attempt
	c.Check(buf.buf[:15], DeepEquals, []byte{'a', 'a', 'a', 'a', 'a', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
}

func (s *downloadSuite) TestDownloadIconTimeout(c *C) {
	const fakeTimeout = 50 * time.Millisecond
	const fakeDelay = 10 * fakeTimeout
	restore := store.MockDownloadIconTimeout(fakeTimeout)
	defer restore()

	n := int64(0)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the response is artificially delayed, which combined with retries on
		// the client side may cause multiple requests to be in-progress at the
		// mock server side

		atomic.AddInt64(&n, 1)
		// wait longer than the client timeout
		time.Sleep(fakeDelay)
		// response should never actually be received
		io.WriteString(w, "response-data")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	startTime := time.Now()
	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", "fake-etag", mockServer.URL, theStore, &buf)
	endTime := time.Now()

	// timeout error will trigger a retry
	c.Check(atomic.LoadInt64(&n) > 3, Equals, true)
	// XXX: context deadline detection is racy, see httputil/retry_test.go in
	// TestRetryRequestTimeoutHandling
	c.Check(err, ErrorMatches, `.* (request canceled|context deadline exceeded)( \(Client.Timeout exceeded while awaiting headers\))?`)
	c.Check(receivedEtag, Equals, "")
	// Check that the timeout duration elapsed before the response returned
	c.Check(endTime.After(startTime.Add(fakeTimeout)), Equals, true)
	// Check that the response returned before waiting for the full response delay
	c.Check(endTime.Before(startTime.Add(fakeDelay)), Equals, true)
}

func (s *downloadSuite) TestDownloadIconCancellation(c *C) {
	ctx, cancel := context.WithCancel(context.Background())

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "foo")
		cancel()
		io.WriteString(w, "bar")
		time.Sleep(10 * time.Millisecond)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(ctx, "foo", "fake-etag", mockServer.URL, theStore, &buf)

	c.Check(n, Equals, 1)
	c.Assert(err, testutil.ErrorIs, context.Canceled)
	c.Check(receivedEtag, Equals, "")
}

func (s *downloadSuite) TestActualDownloadIcon404(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(404)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", "fake-etag", mockServer.URL, theStore, &buf)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &store.DownloadError{})
	c.Check(err.(*store.DownloadError).Code, Equals, 404)
	c.Check(receivedEtag, Equals, "")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestActualDownloadIcon500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)
	var buf SillyBuffer
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", "fake-etag", mockServer.URL, theStore, &buf)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &store.DownloadError{})
	c.Check(err.(*store.DownloadError).Code, Equals, 500)
	c.Check(receivedEtag, Equals, "")
	c.Check(n, Equals, 5)
}

func (s *downloadSuite) TestActualDownloadIcon500Once(c *C) {
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
	receivedEtag, err := store.DownloadIconImpl(context.TODO(), "foo", "fake-etag", mockServer.URL, theStore, &buf)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "response-data")
	c.Check(receivedEtag, Equals, "")
	c.Check(n, Equals, 2)
}
