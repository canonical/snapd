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
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/juju/ratelimit"
	. "gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil))

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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, &store.DownloadOptions{Scheduled: true}))

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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil))

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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil))

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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil))

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
		time.Sleep(10 * time.Millisecond)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	theStore := store.New(&store.Config{}, nil)

	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan string)
	go func() {
		sha3 := ""
		var buf SillyBuffer
		mylog.Check(store.Download(ctx, "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil))
		result <- err.Error()
		close(result)
	}()

	<-syncCh
	cancel()

	err := <-result
	c.Check(n, Equals, 1)
	c.Assert(err, Equals, "the download has been cancelled: context canceled")
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
	mylog.Check(store.Download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, nopeSeeker{&buf}, -1, nil, nil))
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
	mylog.Check(store.Download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil, nil))
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
	mylog.Check(store.Download(context.TODO(), "foo", "sha3", mockServer.URL, nil, theStore, &buf, 0, nil, nil))
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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, &buf, 0, nil, nil))

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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, buf, int64(len("some ")), nil, nil))
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
	mylog.Check(store.Download(context.TODO(), "foo", sha3, mockServer.URL, nil, theStore, buf, int64(len("some ")), nil, nil))
	c.Check(err, IsNil)
	c.Check(buf.String(), Equals, "some data")
	c.Check(n, Equals, 1)
}

func (s *downloadSuite) TestUseDeltas(c *C) {
	// get rid of the mock xdelta3 because we mock all our own stuff
	s.mockXdelta.Restore()
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	restore := release.MockOnClassic(false)
	defer restore()

	origSnapMountDir := dirs.SnapMountDir
	defer func() { dirs.SnapMountDir = origSnapMountDir }()
	dirs.SnapMountDir = c.MkDir()
	exeInCorePath := filepath.Join(dirs.SnapMountDir, "/core/current/usr/bin/xdelta3")
	interpInCorePath := filepath.Join(dirs.SnapMountDir, "/core/current/lib64/ld-linux-x86-64.so.2")

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
		var hostXdelta3Cmd, coreInterpCmd *testutil.MockCmd

		var cleanups []func()

		comment := Commentf("%#v", scenario)

		// setup the env var for the scenario
		os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", scenario.env)
		release.MockOnClassic(scenario.classic)

		// setup binaries for the scenario
		if scenario.exeInCore {
			// We need both the xdelta3 command for determining the interpreter
			// as well as the actual interpreter for executing the basic
			// "xdelta3 config" command.
			// For the interpreter, since that's how we execute xdelta3, mock
			// that as a command, but we don't need to mock the xdelta3 command
			// in the core snap since that doesn't get executed by our fake
			// interpreter. Mocking the interpreter and executing that as a
			// MockCommand has the advantage that it avoids the specific ELF
			// handling that is per-arch, etc. of the real CommandFromSystemSnap
			// implementation.

			coreInterpCmd = testutil.MockCommand(c, interpInCorePath, "")

			r := store.MockSnapdtoolCommandFromSystemSnap(func(name string, args ...string) (*exec.Cmd, error) {
				c.Assert(name, Equals, "/usr/bin/xdelta3")
				c.Assert(args, DeepEquals, []string{"config"})

				// use realistic arguments like what we actually get from
				// snapdtool.CommandFromSystemSnap(), namely the interpreter and
				// a library path which is derived from ld.so - this is
				// artificial and we could use any mocked arguments here, but
				// this more closely matches reality to return something like
				// this.
				interpArgs := append([]string{"--library-path", "/some/dir/from/etc/ld.so", exeInCorePath}, args...)
				return exec.Command(coreInterpCmd.Exe(), interpArgs...), nil
			})
			cleanups = append(cleanups, r)

			// Forget the calls to the interpreter at the end of the test - this
			// deletes the log which otherwise would  continue to persist for
			// each iteration leading to incorrect checks for the calls to the
			// absolute binary that we mocked here, as the log file will be the
			// same for each iteration.
			// For the inverse reason, we don't need to forget calls for the
			// hostXdelta3Cmd mock command, it gets a new dir with a new log
			// file each iteration.
			cleanups = append(cleanups, func() {
				coreInterpCmd.ForgetCalls()
				// note this is currently not needed, since Restore() just
				// resets $PATH, but for an absolute path the $PATH doesn't get
				// modified to begin with in MockCommand, but keep it here just
				// to be safe in case something does ever change
				coreInterpCmd.Restore()
			})
		}

		if scenario.exeInHost {
			// just mock the xdelta3 command directly
			hostXdelta3Cmd = testutil.MockCommand(c, "xdelta3", "")

			// note we don't add a Restore() to cleanups, it is called directly
			// below after the first UseDeltas() but before the second
			// UseDeltas() in order to properly test the caching behavior
		}

		// if there is not meant to be xdelta3 on the host or in core, then set
		// PATH to be empty such that we won't find xdelta3 from the host
		// running these tests
		if !scenario.exeInHost && !scenario.exeInCore {
			os.Setenv("PATH", "")

			// also reset PATH at the end, otherwise an empty PATH leads
			// testutil.MockCommand fails in future iterations that mock a
			// command
			cleanups = append(cleanups, func() {
				os.Setenv("PATH", origPath)
			})
		}

		// run the check for delta usage, we call it twice
		sto := &store.Store{}
		c.Check(sto.UseDeltas(), Equals, scenario.wantDelta, comment)

		// cleanup the files we may have created before calling the function
		// again to ensure that the caching works as expected
		if scenario.exeInCore {
			mylog.Check(os.Remove(interpInCorePath))

		}

		if scenario.exeInHost {
			hostXdelta3Cmd.Restore()
		}

		// also now that we have deleted the mock interpreter and unset the
		// search path, we should still get the same result as above when
		// we call UseDeltas() since it was cached, if it wasn't cached then
		// this would fail
		c.Check(sto.UseDeltas(), Equals, scenario.wantDelta, comment)

		if scenario.wantDelta {
			// if we should have been able to use deltas, make sure we picked
			// the expected one, - if both were true we should have picked the
			// one from core instead of the one from the host first
			if scenario.exeInCore {
				// check that during trying to check whether to use deltas or
				// not, we called the interpreter with the xdelta3 config
				// command too
				c.Check(coreInterpCmd.Calls(), DeepEquals, [][]string{
					{"ld-linux-x86-64.so.2", "--library-path", "/some/dir/from/etc/ld.so", exeInCorePath, "config"},
				}, comment)

				// also check that now after caching the xdelta3 command, it
				// returns the expected format
				expArgs := []string{
					interpInCorePath,
					"--library-path",
					"/some/dir/from/etc/ld.so",
					exeInCorePath,
					"foo",
					"bar",
				}
				// check that the Xdelta3Cmd function we cached uses the
				// interpreter that was returned from CommandFromSystemSnap
				c.Check(sto.Xdelta3Cmd("foo", "bar").Args, DeepEquals, expArgs, comment)

			} else if scenario.exeInHost {
				// similar checks for the host case, except in the host case we
				// just called xdelta3 directly
				c.Check(hostXdelta3Cmd.Calls(), DeepEquals, [][]string{
					{"xdelta3", "config"},
				}, comment)

				// and args are passed to the command cached too
				expArgs := []string{hostXdelta3Cmd.Exe(), "foo", "bar"}
				c.Check(sto.Xdelta3Cmd("foo", "bar").Args, DeepEquals, expArgs, comment)
			}
		} else {
			// quick check that the test case makes sense, if we didn't want
			// deltas, the scenario should have either disabled via an env var,
			// or had both exes missing
			c.Assert((scenario.env == "0") ||
				(!scenario.exeInCore && !scenario.exeInHost),
				Equals, true)
		}

		// cleanup for the next iteration
		for _, r := range cleanups {
			r()
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
	// If more than one matching delta is returned by the store
	// we ignore deltas and do the full download.
	downloads: downloadBehaviour{
		{url: "full-snap-url"},
	},
	info: snap.DownloadInfo{
		DownloadURL: "full-snap-url",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "delta-url", Format: "xdelta3"},
			{DownloadURL: "delta-url-2", Format: "xdelta3"},
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
		restore = store.MockApplyDelta(func(_ *store.Store, name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
			c.Check(deltaInfo, Equals, &testCase.info.Deltas[0])
			mylog.Check(os.WriteFile(targetPath, []byte("snap-content-via-delta"), 0644))

			return nil
		})
		defer restore()

		theStore := store.New(&store.Config{}, nil)
		path := filepath.Join(c.MkDir(), "subdir", "downloaded-file")
		mylog.Check(theStore.Download(context.TODO(), "foo", path, &testCase.info, nil, nil, nil))


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
	mylog.Check(store.Download(context.TODO(), "example-name", "", ts.URL, nil, theStore, &buf, 0, nil, &store.DownloadOptions{RateLimit: 1}))

	c.Check(buf.String(), Equals, canary)
	c.Check(ratelimitReaderUsed, Equals, true)
}
