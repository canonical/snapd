// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package image_test

import (
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

func (s *imageSuite) TestDownloadpOptionsString(c *check.C) {
	tests := []struct {
		opts image.DownloadOptions
		str  string
	}{
		{image.DownloadOptions{LeavePartialOnError: true}, ""},
		{image.DownloadOptions{}, ""},
		{image.DownloadOptions{TargetDir: "/foo"}, `in "/foo"`},
		{image.DownloadOptions{Basename: "foo"}, `to "foo.snap"`},
		{image.DownloadOptions{Channel: "foo"}, `from channel "foo"`},
		{image.DownloadOptions{Revision: snap.R(42)}, `(42)`},
		{image.DownloadOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, `from cohort "…rStUvWxYz"`},
		{image.DownloadOptions{
			TargetDir: "/foo",
			Basename:  "bar",
			Channel:   "baz",
			Revision:  snap.R(13),
			CohortKey: "MSBIc3dwOW9PemozYjRtdzhnY0MwMFh0eFduS0g5UWlDUSAxNTU1NDExNDE1IDBjYzJhNTc1ZjNjOTQ3ZDEwMWE1NTNjZWFkNmFmZDE3ZWJhYTYyNjM4ZWQ3ZGMzNjI5YmU4YjQ3NzAwMjdlMDk=",
		}, `(13) from channel "baz" from cohort "…wMjdlMDk=" to "bar.snap" in "/foo"`}, // note this one is not 'valid' so it's ok if the string is a bit wonky

	}

	for _, t := range tests {
		c.Check(t.opts.String(), check.Equals, t.str)
	}
}

func (s *imageSuite) TestDownloadOptionsValid(c *check.C) {
	tests := []struct {
		opts image.DownloadOptions
		err  error
	}{
		{image.DownloadOptions{}, nil}, // might want to error if no targetdir
		{image.DownloadOptions{TargetDir: "foo"}, nil},
		{image.DownloadOptions{Channel: "foo"}, nil},
		{image.DownloadOptions{Revision: snap.R(42)}, nil},
		{image.DownloadOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, nil},
		{image.DownloadOptions{
			Channel:  "foo",
			Revision: snap.R(42),
		}, nil},
		{image.DownloadOptions{
			Channel:   "foo",
			CohortKey: "bar",
		}, nil},
		{image.DownloadOptions{
			Revision:  snap.R(1),
			CohortKey: "bar",
		}, image.ErrRevisionAndCohort},
		{image.DownloadOptions{
			Basename: "/foo",
		}, image.ErrPathInBase},
	}

	for _, t := range tests {
		t.opts.LeavePartialOnError = true
		c.Check(t.opts.Validate(), check.Equals, t.err)
		t.opts.LeavePartialOnError = false
		c.Check(t.opts.Validate(), check.Equals, t.err)
	}
}

func (s *imageSuite) TestDownloadSnap(c *check.C) {
	// TODO: maybe expand on this (test coverage of DownloadSnap is really bad)

	// env shenanigans
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	debug, hadDebug := os.LookupEnv("SNAPD_DEBUG")
	os.Setenv("SNAPD_DEBUG", "1")
	if hadDebug {
		defer os.Setenv("SNAPD_DEBUG", debug)
	} else {
		defer os.Unsetenv("SNAPD_DEBUG")
	}
	logbuf, restore := logger.MockLogger()
	defer restore()

	s.setupSnaps(c, map[string]string{
		"core": "canonical",
	})

	dlDir := c.MkDir()
	opts := image.DownloadOptions{
		TargetDir: dlDir,
	}
	fn, info, err := s.tsto.DownloadSnap("core", opts)
	c.Assert(err, check.IsNil)
	c.Check(fn, check.Matches, filepath.Join(dlDir, `core_\d+.snap`))
	c.Check(info.SnapName(), check.Equals, "core")

	c.Check(logbuf.String(), check.Matches, `.* DEBUG: Going to download snap "core" `+opts.String()+".\n")
}
