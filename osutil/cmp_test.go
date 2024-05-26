// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package osutil_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type CmpTestSuite struct{}

var _ = Suite(&CmpTestSuite{})

func (ts *CmpTestSuite) TestCmp(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	f := mylog.Check2(os.Create(foo))

	defer f.Close()

	// test FilesAreEqual for various sizes:
	// - bufsz not exceeded
	// - bufsz matches file size
	// - bufsz exceeds file size
	canary := "1234567890123456"
	for _, n := range []int{1, 128 / len(canary), (128 / len(canary)) + 1} {
		for i := 0; i < n; i++ {
			// Pick a smaller buffer size so that the test can complete quicker
			c.Assert(osutil.FilesAreEqualChunked(foo, foo, 128), Equals, true)
			_ := mylog.Check2(f.WriteString(canary))

			f.Sync()
		}
	}
}

func (ts *CmpTestSuite) TestCmpEmptyNeqMissing(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	bar := filepath.Join(tmpdir, "bar")
	f := mylog.Check2(os.Create(foo))

	defer f.Close()
	c.Assert(osutil.FilesAreEqual(foo, bar), Equals, false)
	c.Assert(osutil.FilesAreEqual(bar, foo), Equals, false)
}

func (ts *CmpTestSuite) TestCmpEmptyNeqNonEmpty(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	bar := filepath.Join(tmpdir, "bar")
	f := mylog.Check2(os.Create(foo))

	defer f.Close()
	c.Assert(os.WriteFile(bar, []byte("x"), 0644), IsNil)
	c.Assert(osutil.FilesAreEqual(foo, bar), Equals, false)
	c.Assert(osutil.FilesAreEqual(bar, foo), Equals, false)
}

func (ts *CmpTestSuite) TestCmpStreams(c *C) {
	for _, x := range []struct {
		a string
		b string
		r bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hello", "hell", false},
	} {
		c.Assert(osutil.StreamsEqual(strings.NewReader(x.a), strings.NewReader(x.b)), Equals, x.r)
	}
}

func (s *CmpTestSuite) TestStreamsEqualChunked(c *C) {
	text := "marry had a little lamb"

	// Passing the same stream twice is not mishandled.
	readerA := bytes.NewReader([]byte(text))
	readerB := readerA
	eq := osutil.StreamsEqualChunked(readerA, readerB, 0)
	c.Check(eq, Equals, true)

	// Passing two streams with the same content works as expected. Note that
	// we are using different block sizes to check for additional edge cases.
	for _, chunkSize := range []int{0, 1, len(text) / 2, len(text), len(text) + 1} {
		readerA = bytes.NewReader([]byte(text))
		readerB = bytes.NewReader([]byte(text))
		eq := osutil.StreamsEqualChunked(readerA, readerB, chunkSize)
		c.Check(eq, Equals, true, Commentf("chunk size %d", chunkSize))
	}

	// Passing two streams with unequal contents but equal length works as
	// expected.
	for _, chunkSize := range []int{0, 1, len(text) / 2, len(text), len(text) + 1} {
		comment := Commentf("chunk size %d", chunkSize)
		readerA = bytes.NewReader([]byte(strings.ToLower(text)))
		readerB = bytes.NewReader([]byte(strings.ToUpper(text)))
		eq = osutil.StreamsEqualChunked(readerA, readerB, chunkSize)
		c.Check(eq, Equals, false, comment)
	}

	// Passing two streams which differer by tail only also works as expected.
	for _, chunkSize := range []int{0, 1, len(text) / 2, len(text), len(text) + 1} {
		textWithChangedTail := text[:len(text)-1] + strings.ToUpper(text[len(text)-1:])
		c.Assert(textWithChangedTail, Not(Equals), text)
		c.Assert(len(textWithChangedTail), Equals, len(text))
		comment := Commentf("chunk size %d", chunkSize)
		readerA = bytes.NewReader([]byte(text))
		readerB = bytes.NewReader([]byte(textWithChangedTail))
		eq = osutil.StreamsEqualChunked(readerA, readerB, chunkSize)
		c.Check(eq, Equals, false, comment)
	}

	// Passing two streams with different length works as expected.
	// Note that this is not used by EnsureDirState in practice.
	for _, chunkSize := range []int{0, 1, len(text) / 2, len(text), len(text) + 1} {
		comment := Commentf("A: %q, B: %q, chunk size %d", text, text[:len(text)/2], chunkSize)
		readerA = bytes.NewReader([]byte(text))
		readerB = bytes.NewReader([]byte(text[:len(text)/2]))
		eq = osutil.StreamsEqualChunked(readerA, readerB, chunkSize)
		c.Check(eq, Equals, false, comment)

		// Readers passed the other way around.
		readerA = bytes.NewReader([]byte(text))
		readerB = bytes.NewReader([]byte(text[:len(text)/2]))
		eq = osutil.StreamsEqualChunked(readerB, readerA, chunkSize)
		c.Check(eq, Equals, false, comment)
	}
}
