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

// test helper
func makeGoSourceFile(c *C, content []byte) string {
	fname := filepath.Join(c.MkDir(), "foo.go")
	err := ioutil.WriteFile(fname, []byte(content), 0644)
	c.Assert(err, IsNil)

	return fname
}

func (s *xgettextTestSuite) SetUpTest(c *C) {
	// our test defaults
	opts.NoLocation = false
	opts.AddCommentsTag = "TRANSLATORS:"
	opts.Keyword = "i18n.G"
	opts.KeywordPlural = "i18n.NG"
	opts.SortOutput = true
	opts.PackageName = "snappy"
	opts.MsgIdBugsAddress = "snappy-devel@lists.ubuntu.com"

	// mock time
	formatTime = func() string {
		return "2015-06-30 14:48+0200"
	}
}

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

func (s *xgettextTestSuite) TestProcessFilesSimple(c *C) {
	fname := makeGoSourceFile(c, []byte(`package main

func main() {
    // TRANSLATORS: foo comment
    i18n.G("foo")
}
`))
	err := processFiles([]string{fname})
	c.Assert(err, IsNil)

	c.Assert(msgIDs, DeepEquals, map[string]msgID{
		"foo": msgID{
			msgid:   "foo",
			comment: "#. TRANSLATORS: foo comment\n",
			fname:   fname,
			line:    5,
		},
	})
}

const header = `# SOME DESCRIPTIVE TITLE.
# Copyright (C) YEAR THE PACKAGE'S COPYRIGHT HOLDER
# This file is distributed under the same license as the PACKAGE package.
# FIRST AUTHOR <EMAIL@ADDRESS>, YEAR.
#
#, fuzzy
msgid   ""
msgstr  "Project-Id-Version: snappy\n"
        "Report-Msgid-Bugs-To: snappy-devel@lists.ubuntu.com\n"
        "POT-Creation-Date: 2015-06-30 14:48+0200\n"
        "PO-Revision-Date: YEAR-MO-DA HO:MI+ZONE\n"
        "Last-Translator: FULL NAME <EMAIL@ADDRESS>\n"
        "Language-Team: LANGUAGE <LL@li.org>\n"
        "Language: \n"
        "MIME-Version: 1.0\n"
        "Content-Type: text/plain; charset=CHARSET\n"
        "Content-Transfer-Encoding: 8bit\n"
`

func (s *xgettextTestSuite) TestWriteOutputSimple(c *C) {
	msgIDs = map[string]msgID{
		"foo": msgID{
			msgid: "foo",
			fname: "fname",
			line:  2,
		},
	}
	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#: fname:2
msgid   "foo"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestIntegration(c *C) {
	fname := makeGoSourceFile(c, []byte(`package main

func main() {
    // TRANSLATORS: foo comment
    //              with multiple lines
    i18n.G("foo")

    // this comment has no translators tag
    i18n.G("abc")

    // TRANSLATORS: plural
    i18n.NG("singular", "plural", 99)

    i18n.G("zz %s")
}
`))
	err := processFiles([]string{fname})
	c.Assert(err, IsNil)

	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#: %[2]s:9
msgid   "abc"
msgstr  ""

#: %[2]s:6
#. TRANSLATORS: foo comment
#. with multiple lines
msgid   "foo"
msgstr  ""

#: %[2]s:12
#. TRANSLATORS: plural
msgid   "singular"
msgid_plural   "plural"
msgstr[0]  ""
msgstr[1]  ""

#: %[2]s:14
#, c-format
msgid   "zz %%s"
msgstr  ""

`, header, fname)
	c.Assert(out.String(), Equals, expected)
}
