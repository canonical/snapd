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

package main

import (
	"bytes"
	"fmt"
	"go/token"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type xgettextTestSuite struct {
}

var _ = Suite(&xgettextTestSuite{})

func (s *xgettextTestSuite) TestFormatComment(c *C) {

	var tests = []struct {
		in  string
		out string
	}{
		{in: "// foo ", out: "#. foo\n"},
		{in: "/* foo */", out: "#. foo\n"},
		{in: "/* foo\n */", out: "#. foo\n"},
		{in: "/* foo\nbar   */", out: "#. foo\n#. bar\n"},
	}

	for _, test := range tests {
		c.Assert(formatComment(test.in), Equals, test.out)
	}
}

func (s *xgettextTestSuite) TestprocessSingleGoSource(c *C) {
	src := `package main

func main() {
    // TRANSLATORS: foo comment
    //              with multiple lines
    i18n.G("foo")
}
`
	fname := filepath.Join(c.MkDir(), "foo.go")
	err := ioutil.WriteFile(fname, []byte(src), 0644)
	c.Assert(err, IsNil)

	fset := token.NewFileSet()

	out := bytes.NewBuffer([]byte(""))
	processSingleGoSource(fset, fname, out)

	expected := fmt.Sprintf(`#: %s:6
#. TRANSLATORS: foo comment
#. with multiple lines
msgid "foo"
msgstr ""

`, fname)
	c.Assert(out.String(), Equals, expected)
}
