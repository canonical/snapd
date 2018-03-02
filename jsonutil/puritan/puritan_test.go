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

package puritan_test

import (
	"encoding/json"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/jsonutil/puritan"
)

func Test(t *testing.T) { check.TestingT(t) }

type escapeSuite struct{}

var _ = check.Suite(escapeSuite{})

var table = map[string]string{
	`"hello"`:        "hello",
	`"\u0020"`:       " ",
	`"\uD83D\uDE00"`: "😀",
	`"a\b\r\tb"`:     "ab",
	`"\\\""`:         `\"`,
	// escape sequences (NOTE just the cotrol char is stripped)
	`"\u001b[3mhello\u001b[m"`: "[3mhello[m",
	`"a\u0080z"`:               "az",
	"\"a\u0080z\"":             "az",
	"\"a\u007fz\"":             "az",
	"\"a\u009fz\"":             "az",
	// replacement char
	`"a\uFFFDb"`: "ab",
	// private unicode chars
	`"a\uE000b"`:       "ab",
	`"a\uDB80\uDC00b"`: "ab",
}

func (escapeSuite) TestSimple(c *check.C) {
	var u puritan.SimpleString
	for j, s := range table {
		comm := check.Commentf(j)
		err := json.Unmarshal([]byte(j), &u)
		if j != `"hello"` {
			// shouldn't work
			c.Check(err, check.NotNil, comm)
		} else {
			// should work
			c.Assert(err, check.IsNil, comm)
			c.Check(u.Clean(), check.Equals, s, comm)
		}
	}
}

func (escapeSuite) TestStrings(c *check.C) {
	var u puritan.String
	for j, s := range table {
		comm := check.Commentf(j)
		c.Assert(json.Unmarshal([]byte(j), &u), check.IsNil, comm)
		c.Check(u.Clean(), check.Equals, s, comm)
	}
}

func (escapeSuite) TestBadStrings(c *check.C) {
	table := []string{
		// missing end quotes
		``, `42`, `"`, `"hello`,
		// unescaped quotes
		`"""`,
		// raw control characters
		"\"\x1b[3mhello\x1b[m\"",
		// escapewha
		`"\'"`,
		`"\u20"`,
		// // not 8-bit clean
		// `"\x9f"`,
	}
	var u1 puritan.String
	var u2 puritan.SimpleString
	for _, j := range table {
		comm := check.Commentf("%q", j)
		c.Check(json.Unmarshal([]byte(j), &u1), check.NotNil, comm)
		c.Check(json.Unmarshal([]byte(j), &u2), check.NotNil, comm)
	}
}

func (escapeSuite) TestParagraph(c *check.C) {
	var u puritan.Paragraph
	for j1, v1 := range table {
		for j2, v2 := range table {
			j := j1[:len(j1)-1] + "\\n" + j2[1:]
			s := v1 + "\n" + v2

			comm := check.Commentf(j)
			c.Assert(json.Unmarshal([]byte(j), &u), check.IsNil, comm)
			c.Check(u.Clean(), check.Equals, s, comm)
		}
	}

}

func (escapeSuite) TestSimpleStringSlice(c *check.C) {
	var u puritan.SimpleStringSlice
	c.Assert(json.Unmarshal([]byte(`["abc", "def"]`), &u), check.IsNil)
	c.Check(u.Clean(), check.DeepEquals, []string{"abc", "def"})
}

func (escapeSuite) TestStringSlice(c *check.C) {
	var buf = []byte{'['}
	var expected []string
	for j, s := range table {
		buf = append(buf, j...)
		buf = append(buf, ',')
		expected = append(expected, s)
	}
	buf[len(buf)-1] = ']'

	var u puritan.StringSlice
	c.Assert(json.Unmarshal(buf, &u), check.IsNil)
	c.Check(u.Clean(), check.DeepEquals, expected)
}

func (escapeSuite) TestPriceMap(c *check.C) {
	var u puritan.PriceMap
	c.Assert(json.Unmarshal([]byte(`{"ARS": "3.14","XXY": "-9.99e14"}`), &u), check.IsNil)
	c.Check(u.Clean(), check.DeepEquals, map[string]string{
		"ARS": "3.14",
		"XXY": "-9.99e14",
	})
}

func (escapeSuite) TestOldPriceMap(c *check.C) {
	var u puritan.OldPriceMap
	c.Assert(json.Unmarshal([]byte(`{"ARS": 3.14,"XXY": -9.99e14}`), &u), check.IsNil)
	c.Check(u.Clean(), check.DeepEquals, map[string]float64{
		"ARS": 3.14,
		"XXY": -9.99e14,
	})
}

func (escapeSuite) TestPriceMapBad(c *check.C) {
	var u puritan.PriceMap
	for _, s := range []string{
		`{"USD": "1+IVA"}`,
		`{"XX": "3.14"}`,
	} {
		c.Check(json.Unmarshal([]byte(s), &u), check.NotNil)
	}
}
