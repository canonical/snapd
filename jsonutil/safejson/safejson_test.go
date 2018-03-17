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

package safejson_test

import (
	"encoding/json"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/jsonutil/safejson"
)

func Test(t *testing.T) { check.TestingT(t) }

type escapeSuite struct{}

var _ = check.Suite(escapeSuite{})

var table = map[string]string{
	"null":           "",
	`"hello"`:        "hello",
	`"árbol"`:        "árbol",
	`"\u0020"`:       " ",
	`"\uD83D\uDE00"`: "😀",
	`"a\b\r\tb"`:     "ab",
	`"\\\""`:         `\"`,
	// escape sequences (NOTE just the control char is stripped)
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

func (escapeSuite) TestStrings(c *check.C) {
	var u safejson.String
	for j, s := range table {
		comm := check.Commentf(j)
		c.Assert(json.Unmarshal([]byte(j), &u), check.IsNil, comm)
		c.Check(u.Clean(), check.Equals, s, comm)

		c.Assert(u.UnmarshalJSON([]byte(j)), check.IsNil, comm)
		c.Check(u.Clean(), check.Equals, s, comm)
	}
}

func (escapeSuite) TestBadStrings(c *check.C) {
	var u1 safejson.String

	cc0 := make([][]byte, 0x20)
	for i := range cc0 {
		cc0[i] = []byte{'"', byte(i), '"'}
	}
	badesc := make([][]byte, 0, 0x7f-0x21-9)
	for c := byte('!'); c <= '~'; c++ {
		switch c {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
			continue
		default:
			badesc = append(badesc, []byte{'"', '\\', c, '"'})
		}
	}

	table := map[string][][]byte{
		// these are from json itself (so we're not checking them):
		"invalid character '.+' in string literal":     cc0,
		"invalid character '.+' in string escape code": badesc,
		`invalid character '.+' in \\u .*`:             {[]byte(`"\u02"`), []byte(`"\u02zz"`)},
		"invalid character '\"' after top-level value": {[]byte(`"""`)},
		"unexpected end of JSON input":                 {[]byte(`"\"`)},
	}

	for e, js := range table {
		for _, j := range js {
			comm := check.Commentf("%q", j)
			c.Check(json.Unmarshal(j, &u1), check.ErrorMatches, e, comm)
		}
	}

	table = map[string][][]byte{
		// these are from our lib
		`missing string delimiters.*`:                 {{}, {'"'}},
		`unexpected control character at 0 in "\\.+"`: cc0,
		`unknown escape '.' at 1 of "\\."`:            badesc,
		`badly formed \\u escape.*`: {
			[]byte(`"\u02"`), []byte(`"\u02zz"`), []byte(`"a\u02xxz"`),
			[]byte(`"\uD83Da"`), []byte(`"\uD83Da\u20"`), []byte(`"\uD83Da\u20zzz"`),
		},
		`unexpected unescaped quote at 0 in """`:            {[]byte(`"""`)},
		`unexpected end of string \(trailing backslash\).*`: {[]byte(`"\"`)},
	}

	for e, js := range table {
		for _, j := range js {
			comm := check.Commentf("%q", j)
			c.Check(u1.UnmarshalJSON(j), check.ErrorMatches, e, comm)
		}
	}
}

func (escapeSuite) TestParagraph(c *check.C) {
	var u safejson.Paragraph
	for j1, v1 := range table {
		for j2, v2 := range table {
			if j2 == "null" && j1 != "null" {
				continue
			}

			var j, s string

			if j1 == "null" {
				j = j2
				s = v2
			} else {
				j = j1[:len(j1)-1] + "\\n" + j2[1:]
				s = v1 + "\n" + v2
			}

			comm := check.Commentf(j)
			c.Assert(json.Unmarshal([]byte(j), &u), check.IsNil, comm)
			c.Check(u.Clean(), check.Equals, s, comm)
		}
	}

}
