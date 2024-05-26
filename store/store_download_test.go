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
	"crypto"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type storeDownloadSuite struct {
	baseStoreSuite

	store *store.Store

	localUser *auth.UserState

	mockXDelta *testutil.MockCmd
}

var _ = Suite(&storeDownloadSuite{})

func (s *storeDownloadSuite) SetUpTest(c *C) {
	s.baseStoreSuite.SetUpTest(c)

	c.Assert(os.MkdirAll(dirs.SnapMountDir, 0755), IsNil)

	s.store = store.New(nil, nil)

	s.localUser = &auth.UserState{
		ID:       11,
		Username: "test-user",
		Macaroon: "snapd-macaroon",
	}

	s.mockXDelta = testutil.MockCommand(c, "xdelta3", "")
	s.AddCleanup(s.mockXDelta.Restore)

	store.MockDownloadRetryStrategy(&s.BaseTest, retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	)))
}

func (s *storeDownloadSuite) TestDownloadOK(c *C) {
	expectedContent := []byte("I was downloaded")

	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		c.Check(url, Equals, "URL")
		w.Write(expectedContent)
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = int64(len(expectedContent))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, nil))

	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeDownloadSuite) TestDownloadRangeRequest(c *C) {
	partialContentStr := "partial content "
	missingContentStr := "was downloaded"
	expectedContentStr := partialContentStr + missingContentStr

	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		c.Check(resume, Equals, int64(len(partialContentStr)))
		c.Check(url, Equals, "URL")
		w.Write([]byte(missingContentStr))
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Sha3_384 = "abcdabcd"
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(os.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644))

	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))


	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeDownloadSuite) TestResumeOfCompleted(c *C) {
	expectedContentStr := "nothing downloaded"

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Sha3_384 = fmt.Sprintf("%x", sha3.Sum384([]byte(expectedContentStr)))
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(os.WriteFile(targetFn+".partial", []byte(expectedContentStr), 0644))

	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))


	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeDownloadSuite) TestDownloadEOFHandlesResumeHashCorrectly(c *C) {
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
		if len(r.Header["Range"]) > 0 {
			w.WriteHeader(206)
		}
		w.Write(buf[len(buf)-5:])
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))

	c.Assert(targetFn, testutil.FileEquals, buf)
	c.Assert(s.logbuf.String(), Matches, "(?s).*Retrying .* attempt 2, .*")
}

func (s *storeDownloadSuite) TestDownloadRetryHashErrorIsFullyRetried(c *C) {
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
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))


	c.Assert(targetFn, testutil.FileEquals, buf)

	c.Assert(s.logbuf.String(), Matches, "(?s).*Retrying .* attempt 2, .*")
}

func (s *storeDownloadSuite) TestResumeOfCompletedRetriedOnHashFailure(c *C) {
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
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	c.Assert(os.WriteFile(targetFn+".partial", badbuf, 0644), IsNil)
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))


	c.Assert(targetFn, testutil.FileEquals, buf)

	c.Assert(s.logbuf.String(), Matches, "(?s).*sha3-384 mismatch.*")
}

func (s *storeDownloadSuite) TestResumeOfTooMuchDataWorks(c *C) {
	var mockServer *httptest.Server

	// our mock download content
	snapContent := "snap-content"
	// the partial file has too much data
	tooMuchLocalData := "way-way-way-too-much-snap-content"

	h := crypto.SHA3_384.New()
	io.Copy(h, bytes.NewBufferString(snapContent))

	n := 0
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Write([]byte(snapContent))
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = int64(len(snapContent))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	c.Assert(os.WriteFile(targetFn+".partial", []byte(tooMuchLocalData), 0644), IsNil)
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))

	c.Assert(n, Equals, 1)

	c.Assert(targetFn, testutil.FileEquals, snapContent)

	c.Assert(s.logbuf.String(), Matches, "(?s).*sha3-384 mismatch.*")
}

func (s *storeDownloadSuite) TestDownloadRetryHashErrorIsFullyRetriedOnlyOnce(c *C) {
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
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = "invalid-hash"
	snap.Size = int64(len("something invalid"))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))

	_, ok := err.(store.HashError)
	c.Assert(ok, Equals, true)
	// ensure we only retried once (as these downloads might be big)
	c.Assert(n, Equals, 2)
}

func (s *storeDownloadSuite) TestDownloadRangeRequestRetryOnHashError(c *C) {
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
	snap.DownloadURL = "URL"
	snap.Sha3_384 = ""
	snap.Size = int64(len(expectedContentStr))

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(os.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644))

	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))

	c.Assert(n, Equals, 2)

	c.Assert(targetFn, testutil.FileEquals, expectedContentStr)
}

func (s *storeDownloadSuite) TestDownloadRangeRequestFailOnHashError(c *C) {
	partialContentStr := "partial content "

	n := 0
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		n++
		return store.NewHashError("foo", "1234", "5678")
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Sha3_384 = ""
	snap.Size = int64(len(partialContentStr) + 1)

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(os.WriteFile(targetFn+".partial", []byte(partialContentStr), 0644))

	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `sha3-384 mismatch for "foo": got 1234 but expected 5678`)
	c.Assert(n, Equals, 2)
}

func (s *storeDownloadSuite) TestDownloadWithUser(c *C) {
	expectedContent := []byte("I was downloaded")
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, _ *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		// check user is pass and auth url is used
		c.Check(user, Equals, s.user)
		c.Check(url, Equals, "URL")

		w.Write(expectedContent)
		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = int64(len(expectedContent))

	path := filepath.Join(c.MkDir(), "downloaded-file")
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, s.user, nil))

	defer os.Remove(path)

	c.Assert(path, testutil.FileEquals, expectedContent)
}

func (s *storeDownloadSuite) TestDownloadFails(c *C) {
	var tmpfile *os.File
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		tmpfile = w.(*os.File)
		return fmt.Errorf("uh, it failed")
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = 1
	// simulate a failed download
	path := filepath.Join(c.MkDir(), "downloaded-file")
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, nil))
	c.Assert(err, ErrorMatches, "uh, it failed")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
	// ... and not because it succeeded either
	c.Assert(osutil.FileExists(path), Equals, false)
}

func (s *storeDownloadSuite) TestDownloadFailsLeavePartial(c *C) {
	var tmpfile *os.File
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		tmpfile = w.(*os.File)
		w.Write([]byte{'X'}) // so it's not empty
		return fmt.Errorf("uh, it failed")
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = 1
	// simulate a failed download
	path := filepath.Join(c.MkDir(), "downloaded-file")
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, &store.DownloadOptions{LeavePartialOnError: true}))
	c.Assert(err, ErrorMatches, "uh, it failed")
	// ... and ensure that the tempfile is *NOT* removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, true)
	// ... but the target path isn't there
	c.Assert(osutil.FileExists(path), Equals, false)
}

func (s *storeDownloadSuite) TestDownloadFailsDoesNotLeavePartialIfEmpty(c *C) {
	var tmpfile *os.File
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		tmpfile = w.(*os.File)
		// no write, so the partial is empty
		return fmt.Errorf("uh, it failed")
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = 1
	// simulate a failed download
	path := filepath.Join(c.MkDir(), "downloaded-file")
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, &store.DownloadOptions{LeavePartialOnError: true}))
	c.Assert(err, ErrorMatches, "uh, it failed")
	// ... and ensure that the tempfile *is* removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
	// ... and the target path isn't there
	c.Assert(osutil.FileExists(path), Equals, false)
}

func (s *storeDownloadSuite) TestDownloadSyncFails(c *C) {
	var tmpfile *os.File
	restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, s *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
		tmpfile = w.(*os.File)
		w.Write([]byte("sync will fail"))
		mylog.Check(tmpfile.Close())

		return nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = int64(len("sync will fail"))

	// simulate a failed sync
	path := filepath.Join(c.MkDir(), "downloaded-file")
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, nil))
	c.Assert(err, ErrorMatches, `(sync|fsync:) .*`)
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
	// ... because it's been renamed to the target path already
	c.Assert(osutil.FileExists(path), Equals, true)
}

var downloadDeltaTests = []struct {
	info        snap.DownloadInfo
	withUser    bool
	format      string
	expectedURL string
	expectError bool
}{{
	// No user delta download.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	format:      "xdelta3",
	expectedURL: "delta-url",
	expectError: false,
}, {
	// With user detla download.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
		},
	},
	withUser:    true,
	format:      "xdelta3",
	expectedURL: "delta-url",
	expectError: false,
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
	format:      "xdelta3",
	expectedURL: "",
	expectError: true,
}, {
	// If the supported format isn't available, an error is returned.
	info: snap.DownloadInfo{
		Sha3_384: "sha3",
		Deltas: []snap.DeltaInfo{
			{DownloadURL: "xdelta3-delta-url", Format: "xdelta3", FromRevision: 24, ToRevision: 26},
			{DownloadURL: "ydelta-delta-url", Format: "ydelta", FromRevision: 24, ToRevision: 26},
		},
	},
	format:      "bsdiff",
	expectedURL: "",
	expectError: true,
}}

func (s *storeDownloadSuite) TestDownloadDelta(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	dauthCtx := &testDauthContext{c: c}
	sto := store.New(nil, dauthCtx)

	for _, testCase := range downloadDeltaTests {
		sto.SetDeltaFormat(testCase.format)
		restore := store.MockDownload(func(ctx context.Context, name, sha3, url string, user *auth.UserState, _ *store.Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *store.DownloadOptions) error {
			c.Check(dlOpts, DeepEquals, &store.DownloadOptions{Scheduled: true})
			expectedUser := s.user
			if !testCase.withUser {
				expectedUser = nil
			}
			c.Check(user, Equals, expectedUser)
			c.Check(url, Equals, testCase.expectedURL)
			w.Write([]byte("I was downloaded"))
			return nil
		})
		defer restore()

		w := mylog.Check2(os.CreateTemp("", ""))

		defer os.Remove(w.Name())

		authedUser := s.user
		if !testCase.withUser {
			authedUser = nil
		}
		mylog.Check(sto.DownloadDelta("snapname", &testCase.info, w, nil, authedUser, &store.DownloadOptions{Scheduled: true}))

		if testCase.expectError {
			c.Assert(err, NotNil)
		} else {

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

func (s *storeDownloadSuite) TestApplyDelta(c *C) {
	for _, testCase := range applyDeltaTests {
		name := "foo"
		currentSnapName := fmt.Sprintf("%s_%d.snap", name, testCase.currentRevision)
		currentSnapPath := filepath.Join(dirs.SnapBlobDir, currentSnapName)
		targetSnapName := fmt.Sprintf("%s_%d.snap", name, testCase.deltaInfo.ToRevision)
		targetSnapPath := filepath.Join(dirs.SnapBlobDir, targetSnapName)
		mylog.Check(os.MkdirAll(filepath.Dir(currentSnapPath), 0755))

		mylog.Check(os.WriteFile(currentSnapPath, nil, 0644))

		deltaPath := filepath.Join(dirs.SnapBlobDir, "the.delta")
		mylog.Check(os.WriteFile(deltaPath, nil, 0644))

		// When testing a case where the call to the external
		// xdelta3 is successful,
		// simulate the resulting .partial.
		if testCase.error == "" {
			mylog.Check(os.WriteFile(targetSnapPath+".partial", nil, 0644))

		}

		// make a fresh store object to circumvent the caching of xdelta3 info
		// between test cases
		sto := &store.Store{}
		mylog.Check(store.ApplyDelta(sto, name, deltaPath, &testCase.deltaInfo, targetSnapPath, ""))

		if testCase.error == "" {

			c.Assert(s.mockXDelta.Calls(), DeepEquals, [][]string{
				// since we don't cache xdelta3 in this test, we always check if
				// xdelta3 config is successful before using xdelta3 (and at
				// that point cache xdelta3 and don't call config again)
				{"xdelta3", "config"},
				{"xdelta3", "-d", "-s", currentSnapPath, deltaPath, targetSnapPath + ".partial"},
			})
			c.Assert(osutil.FileExists(targetSnapPath+".partial"), Equals, false)
			st := mylog.Check2(os.Stat(targetSnapPath))

			c.Check(st.Mode(), Equals, os.FileMode(0600))
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

type cacheObserver struct {
	inCache map[string]bool

	gets []string
	puts []string
}

func (co *cacheObserver) Get(cacheKey, targetPath string) bool {
	co.gets = append(co.gets, fmt.Sprintf("%s:%s", cacheKey, targetPath))
	return co.inCache[cacheKey]
}

func (co *cacheObserver) GetPath(cacheKey string) string {
	return ""
}

func (co *cacheObserver) Put(cacheKey, sourcePath string) error {
	co.puts = append(co.puts, fmt.Sprintf("%s:%s", cacheKey, sourcePath))
	return nil
}

func (s *storeDownloadSuite) TestDownloadCacheHit(c *C) {
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
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, nil))


	c.Check(obs.gets, DeepEquals, []string{fmt.Sprintf("%s:%s", snap.Sha3_384, path)})
	c.Check(obs.puts, IsNil)
}

func (s *storeDownloadSuite) TestDownloadCacheMiss(c *C) {
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
	mylog.Check(s.store.Download(s.ctx, "foo", path, &snap.DownloadInfo, nil, nil, nil))

	c.Check(downloadWasCalled, Equals, true)

	c.Check(obs.gets, DeepEquals, []string{fmt.Sprintf("the-snaps-sha3_384:%s", path)})
	c.Check(obs.puts, DeepEquals, []string{fmt.Sprintf("the-snaps-sha3_384:%s", path)})
}

func (s *storeDownloadSuite) TestDownloadStreamOK(c *C) {
	expectedContent := []byte("I was downloaded")
	restore := store.MockDoDownloadReq(func(ctx context.Context, url *url.URL, cdnHeader string, resume int64, s *store.Store, user *auth.UserState) (*http.Response, error) {
		c.Check(url.String(), Equals, "URL")
		r := &http.Response{
			Body: io.NopCloser(bytes.NewReader(expectedContent[resume:])),
		}
		if resume > 0 {
			r.StatusCode = 206
		} else {
			r.StatusCode = 200
		}
		return r, nil
	})
	defer restore()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = int64(len(expectedContent))

	stream, status := mylog.Check3(s.store.DownloadStream(context.TODO(), "foo", &snap.DownloadInfo, 0, nil))

	c.Assert(status, Equals, 200)

	buf := new(bytes.Buffer)
	buf.ReadFrom(stream)
	c.Check(buf.String(), Equals, string(expectedContent))

	stream, status = mylog.Check3(s.store.DownloadStream(context.TODO(), "foo", &snap.DownloadInfo, 2, nil))

	c.Check(status, Equals, 206)

	buf = new(bytes.Buffer)
	buf.ReadFrom(stream)
	c.Check(buf.String(), Equals, string(expectedContent[2:]))
}

func (s *storeDownloadSuite) TestDownloadStreamCachedOK(c *C) {
	expectedContent := []byte("I was NOT downloaded")
	defer store.MockDoDownloadReq(func(context.Context, *url.URL, string, int64, *store.Store, *auth.UserState) (*http.Response, error) {
		c.Fatalf("should not be here")
		return nil, nil
	})()

	c.Assert(os.MkdirAll(dirs.SnapDownloadCacheDir, 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDownloadCacheDir, "sha3_384-of-foo"), expectedContent, 0600), IsNil)

	cache := store.NewCacheManager(dirs.SnapDownloadCacheDir, 1)
	defer s.store.MockCacher(cache)()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = "URL"
	snap.Size = int64(len(expectedContent))
	snap.Sha3_384 = "sha3_384-of-foo"

	stream, status := mylog.Check3(s.store.DownloadStream(context.TODO(), "foo", &snap.DownloadInfo, 0, nil))
	c.Check(err, IsNil)
	c.Check(status, Equals, 200)

	buf := new(bytes.Buffer)
	buf.ReadFrom(stream)
	c.Check(buf.String(), Equals, string(expectedContent))

	stream, status = mylog.Check3(s.store.DownloadStream(context.TODO(), "foo", &snap.DownloadInfo, 2, nil))

	c.Check(status, Equals, 206)

	buf = new(bytes.Buffer)
	buf.ReadFrom(stream)
	c.Check(buf.String(), Equals, string(expectedContent[2:]))
}

func (s *storeDownloadSuite) TestDownloadTimeout(c *C) {
	var mockServer *httptest.Server

	restore := store.MockDownloadSpeedParams(1*time.Second, 32768)
	defer restore()

	// our mock download content
	buf := make([]byte, 65535)

	h := crypto.SHA3_384.New()
	io.Copy(h, bytes.NewBuffer(buf))

	quit := make(chan bool)
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Length", fmt.Sprintf("%d", len(buf)))
		w.WriteHeader(200)

		// push enough data to fill in internal buffers, so that download code
		// hits io.Copy over the body and gets stuck there, and not immediately
		// on doRequest.
		w.Write(buf[:20000])

		// block the handler
		select {
		case <-quit:
		case <-time.After(10 * time.Second):
			c.Fatalf("unexpected server timeout")
		}
		mockServer.CloseClientConnections()
	}))

	c.Assert(mockServer, NotNil)

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = fmt.Sprintf("%x", h.Sum(nil))
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))
	ok, speed := store.IsTransferSpeedError(err)
	c.Assert(ok, Equals, true)
	// in reality speed can be 0, but here it's an extra quick check.
	c.Check(speed > 1, Equals, true)
	c.Check(speed < 32768, Equals, true)
	close(quit)
	defer mockServer.Close()
}

func (s *storeDownloadSuite) TestTransferSpeedMonitoringWriterHappy(c *C) {
	if os.Getenv("SNAPD_SKIP_SLOW_TESTS") != "" {
		c.Skip("skipping slow test")
	}

	origCtx := context.TODO()
	w, ctx := store.NewTransferSpeedMonitoringWriterAndContext(origCtx, 50*time.Millisecond, 1)

	data := []byte{0, 0, 0, 0, 0}
	quit := w.Monitor()

	// write a few bytes every ~5ms, this should satisfy >=1 speed in 50ms
	// measure windows defined above; 100 iterations ensures we hit a few
	// measurement windows.
	for i := 0; i < 100; i++ {
		n := mylog.Check2(w.Write(data))

		c.Assert(n, Equals, len(data))
		time.Sleep(5 * time.Millisecond)
	}
	close(quit)
	c.Check(store.Cancelled(ctx), Equals, false)
	c.Check(w.Err(), IsNil)

	// we should hit at least 100*5/50 = 10 measurement windows
	c.Assert(w.MeasuredWindowsCount() >= 10, Equals, true, Commentf("%d", w.MeasuredWindowsCount()))
}

func (s *storeDownloadSuite) TestTransferSpeedMonitoringWriterUnhappy(c *C) {
	if os.Getenv("SNAPD_SKIP_SLOW_TESTS") != "" {
		c.Skip("skipping slow test")
	}

	origCtx := context.TODO()
	w, ctx := store.NewTransferSpeedMonitoringWriterAndContext(origCtx, 50*time.Millisecond, 1000)

	data := []byte{0}
	quit := w.Monitor()

	// write just one byte every ~5ms, this will trigger download timeout
	// since the writer expects 1000 bytes per 50ms as defined above.
	for i := 0; i < 100; i++ {
		n := mylog.Check2(w.Write(data))

		c.Assert(n, Equals, len(data))
		time.Sleep(5 * time.Millisecond)
	}
	close(quit)
	c.Check(store.Cancelled(ctx), Equals, true)
	terr, _ := store.IsTransferSpeedError(w.Err())
	c.Assert(terr, Equals, true)
	c.Check(w.Err(), ErrorMatches, "download too slow: .* bytes/sec")
}

func (s *storeDownloadSuite) TestDownloadTimeoutOnHeaders(c *C) {
	restore := httputil.MockResponseHeaderTimeout(250 * time.Millisecond)
	defer restore()

	var mockServer *httptest.Server

	quit := make(chan bool)
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// block the handler, do not send response headers.
		select {
		case <-quit:
		case <-time.After(30 * time.Second):
			// we expect to hit ResponseHeaderTimeout first
			c.Fatalf("unexpected")
		}
		mockServer.CloseClientConnections()
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.DownloadURL = mockServer.URL
	snap.Sha3_384 = "1234"
	snap.Size = 50000

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, nil, nil))
	close(quit)
	c.Assert(err, ErrorMatches, `.*net/http: timeout awaiting response headers`)
}

func (s *storeDownloadSuite) TestDownloadRedirectHideAuthHeaders(c *C) {
	var mockStoreServer, mockCdnServer *httptest.Server

	mockStoreServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, expectedAuthorization(c, s.user))
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)
		http.Redirect(w, r, mockCdnServer.URL, 302)
	}))
	c.Assert(mockStoreServer, NotNil)
	defer mockStoreServer.Close()

	mockCdnServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, exists := r.Header["Authorization"]
		c.Check(exists, Equals, false)
		_, exists = r.Header["X-Device-Authorization"]
		c.Check(exists, Equals, false)
		io.WriteString(w, "test-download")
	}))
	c.Assert(mockCdnServer, NotNil)
	defer mockCdnServer.Close()

	snap := &snap.Info{}
	snap.DownloadURL = mockStoreServer.URL

	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{}, dauthCtx)

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(sto.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, s.user, nil))
	c.Assert(err, Equals, nil)
	c.Assert(targetFn, testutil.FileEquals, "test-download")
}

func (s *storeDownloadSuite) TestDownloadNoCheckRedirectPanic(c *C) {
	restore := store.MockHttputilNewHTTPClient(func(opts *httputil.ClientOptions) *http.Client {
		client := httputil.NewHTTPClient(opts)
		client.CheckRedirect = nil
		return client
	})
	defer restore()

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	downloadFunc := func() {
		s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo{}, nil, nil, nil)
	}
	c.Assert(downloadFunc, PanicMatches, "internal error: the httputil.NewHTTPClient-produced http.Client must have CheckRedirect defined")
}

func (s *storeDownloadSuite) TestDownloadInfiniteRedirect(c *C) {
	n := 0
	var mockServer *httptest.Server

	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// n = 0  -> initial request
		// n = 10 -> max redirects
		// n = 11 -> exceeded max redirects
		c.Assert(n, testutil.IntNotEqual, 11)
		n++
		http.Redirect(w, r, mockServer.URL, 302)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := &snap.Info{}
	snap.DownloadURL = mockServer.URL

	targetFn := filepath.Join(c.MkDir(), "foo_1.0_all.snap")
	mylog.Check(s.store.Download(s.ctx, "foo", targetFn, &snap.DownloadInfo, nil, s.user, nil))
	c.Assert(err, ErrorMatches, fmt.Sprintf("Get %q: stopped after 10 redirects", mockServer.URL))
}
