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

package strutil_test

import (
	"regexp"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

type mcSuite struct{}

var _ = check.Suite(&mcSuite{})

const out = `

Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols, skipping

Write on output file failed because No space left on device

Hello I am a happy line that does not mention failure.

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols.bin, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdso32.so, skipping

Write on output file failed because No space left on device

La la la.

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdso64.so, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdsox32.so, skipping

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/snap/manifest.yaml, skipping

🦄🌈💩

Write on output file failed because No space left on device

writer: failed to write data block 0

Failed to write /tmp/1/snap/snapcraft.yaml, skipping
`

var (
	thisRegexp     = regexp.MustCompile("(?m).*[Ff]ailed.*")
	nilRegexpEquiv = regexp.MustCompile("(?m).+")
)

func (mcSuite) TestMatchCounterFull(c *check.C) {
	// check a single write
	expected := thisRegexp.FindAllString(out, 3)
	w := &strutil.MatchCounter{Regexp: thisRegexp, N: 3}
	_ := mylog.Check2(w.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count := w.Matches()
	c.Check(count, check.Equals, 19)
	c.Assert(matches, check.DeepEquals, expected)

	expectedLast := []string{
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
		"Failed to write /tmp/1/snap/snapcraft.yaml, skipping",
	}
	wLast := &strutil.MatchCounter{Regexp: thisRegexp, N: 3, LastN: true}
	_ = mylog.Check2(wLast.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count = wLast.Matches()
	c.Check(count, check.Equals, 19)
	c.Assert(matches, check.DeepEquals, expectedLast)
}

func (mcSuite) TestMatchCounterPartials(c *check.C) {
	// now we know the whole thing matches expected, we check partials
	buf := []byte(out)
	expected := []string{
		"Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols, skipping",
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
	}
	expectedLast := []string{
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
		"Failed to write /tmp/1/snap/snapcraft.yaml, skipping",
	}

	for _, keepLast := range []bool{false, true} {
		for step := 1; step < 100; step++ {
			w := &strutil.MatchCounter{Regexp: thisRegexp, N: 3, LastN: keepLast}
			var i int
			for i = 0; i+step < len(buf); i += step {
				_ := mylog.Check2(w.Write(buf[i : i+step]))
				c.Assert(err, check.IsNil, check.Commentf("step:%d i:%d", step, i))
			}
			_ := mylog.Check2(w.Write(buf[i:]))
			c.Assert(err, check.IsNil, check.Commentf("step:%d tail", step))
			matches, count := w.Matches()
			c.Check(count, check.Equals, 19, check.Commentf("step:%d", step))
			if !keepLast {
				c.Check(matches, check.DeepEquals, expected, check.Commentf("step:%d", step))
			} else {
				c.Check(matches, check.DeepEquals, expectedLast, check.Commentf("step:%d", step))
			}
		}
	}
}

func (mcSuite) TestMatchCounterPartialsReusingBuffer(c *check.C) {
	// now we know the whole thing matches expected, we check partials
	buf := []byte(out)
	expected := []string{
		"Failed to write /tmp/1/modules/4.4.0-112-generic/modules.symbols, skipping",
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
	}

	for step := 1; step < 100; step++ {
		wbuf := make([]byte, step)
		w := &strutil.MatchCounter{Regexp: thisRegexp, N: 3}
		var i int
		for i = 0; i+step < len(buf); i += step {
			copy(wbuf, buf[i:])
			_ := mylog.Check2(w.Write(wbuf))
			c.Assert(err, check.IsNil, check.Commentf("step:%d i:%d", step, i))
		}
		wbuf = wbuf[:len(buf[i:])]
		copy(wbuf, buf[i:])
		_ := mylog.Check2(w.Write(wbuf))
		c.Assert(err, check.IsNil, check.Commentf("step:%d tail", step))
		matches, count := w.Matches()
		c.Assert(count, check.Equals, 19, check.Commentf("step:%d", step))
		c.Assert(matches, check.DeepEquals, expected, check.Commentf("step:%d", step))
	}
}

func (mcSuite) TestMatchCounterZero(c *check.C) {
	w := &strutil.MatchCounter{Regexp: thisRegexp, N: 0}
	_ := mylog.Check2(w.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count := w.Matches()
	c.Check(count, check.Equals, 19)
	c.Assert(matches, check.HasLen, 0)

	wLast := &strutil.MatchCounter{Regexp: thisRegexp, N: 0, LastN: true}
	_ = mylog.Check2(wLast.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count = w.Matches()
	c.Check(count, check.Equals, 19)
	c.Assert(matches, check.HasLen, 0)
}

func (mcSuite) TestMatchCounterNegative(c *check.C) {
	expected := thisRegexp.FindAllString(out, -1)

	w := &strutil.MatchCounter{Regexp: thisRegexp, N: -1}
	_ := mylog.Check2(w.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count := w.Matches()
	c.Check(count, check.Equals, 19)
	c.Check(count, check.Equals, len(matches))
	c.Assert(matches, check.DeepEquals, expected)

	// always keep all lines
	wLast := &strutil.MatchCounter{Regexp: thisRegexp, N: -1, LastN: true}
	_ = mylog.Check2(wLast.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count = w.Matches()
	c.Check(count, check.Equals, 19)
	c.Check(count, check.Equals, len(matches))
	c.Assert(matches, check.DeepEquals, expected)
}

func (mcSuite) TestMatchCounterNilRegexpFull(c *check.C) {
	expected := nilRegexpEquiv.FindAllString(out, -1)
	w := &strutil.MatchCounter{N: -1}
	_ := mylog.Check2(w.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count := w.Matches()
	c.Check(count, check.Equals, len(matches))
	c.Check(len(matches), check.Equals, len(expected))
	c.Assert(matches, check.DeepEquals, expected)
}

func (mcSuite) TestMatchCounterNilRegexpLimited(c *check.C) {
	expected := nilRegexpEquiv.FindAllString(out, 10)
	w := &strutil.MatchCounter{N: 10}
	_ := mylog.Check2(w.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count := w.Matches()
	c.Check(count, check.Equals, 22)
	c.Check(len(matches), check.Equals, len(expected))
	c.Assert(matches, check.DeepEquals, expected)

	expectedLast := []string{
		// "writer: failed to write data block 0",
		// "Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdso64.so, skipping",
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
		"Failed to write /tmp/1/modules/4.4.0-112-generic/vdso/vdsox32.so, skipping",
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
		"Failed to write /tmp/1/snap/manifest.yaml, skipping",
		"🦄🌈💩",
		"Write on output file failed because No space left on device",
		"writer: failed to write data block 0",
		"Failed to write /tmp/1/snap/snapcraft.yaml, skipping",
	}
	wLast := &strutil.MatchCounter{N: 10, LastN: true}
	_ = mylog.Check2(wLast.Write([]byte(out)))
	c.Assert(err, check.IsNil)
	matches, count = wLast.Matches()
	c.Check(count, check.Equals, 22)
	c.Check(len(matches), check.Equals, len(expectedLast))
	c.Assert(matches, check.DeepEquals, expectedLast)
}

func (mcSuite) TestMatchCounterNilRegexpPartials(c *check.C) {
	expected := nilRegexpEquiv.FindAllString(out, 3)

	buf := []byte(out)
	for step := 1; step < 100; step++ {
		w := &strutil.MatchCounter{N: 3}
		var i int
		for i = 0; i+step < len(buf); i += step {
			_ := mylog.Check2(w.Write(buf[i : i+step]))
			c.Assert(err, check.IsNil, check.Commentf("step:%d i:%d", step, i))
		}
		_ := mylog.Check2(w.Write(buf[i:]))
		c.Assert(err, check.IsNil, check.Commentf("step:%d tail", step))
		matches, count := w.Matches()
		c.Check(count, check.Equals, 22, check.Commentf("step:%d", step))
		c.Check(matches, check.DeepEquals, expected, check.Commentf("step:%d", step))
	}
}
