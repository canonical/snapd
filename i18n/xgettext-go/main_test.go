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
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func TestT(t *testing.T) { TestingT(t) }

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
	opts.MsgIDBugsAddress = "snappy-devel@lists.ubuntu.com"

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

	c.Assert(msgIDs, DeepEquals, map[string][]msgID{
		"foo": {
			{
				comment: "#. TRANSLATORS: foo comment\n",
				fname:   fname,
				line:    5,
			},
		},
	})
}

func (s *xgettextTestSuite) TestProcessFilesMultiple(c *C) {
	fname := makeGoSourceFile(c, []byte(`package main

func main() {
    // TRANSLATORS: foo comment
    i18n.G("foo")

    // TRANSLATORS: bar comment
    i18n.G("foo")
}
`))
	err := processFiles([]string{fname})
	c.Assert(err, IsNil)

	c.Assert(msgIDs, DeepEquals, map[string][]msgID{
		"foo": {
			{
				comment: "#. TRANSLATORS: foo comment\n",
				fname:   fname,
				line:    5,
			},
			{
				comment: "#. TRANSLATORS: bar comment\n",
				fname:   fname,
				line:    8,
			},
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
	msgIDs = map[string][]msgID{
		"foo": {
			{
				fname:   "fname",
				line:    2,
				comment: "#. foo\n",
			},
		},
	}
	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#. foo
#: fname:2
msgid   "foo"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestWriteOutputMultiple(c *C) {
	msgIDs = map[string][]msgID{
		"foo": {
			{
				fname:   "fname",
				line:    2,
				comment: "#. comment1\n",
			},
			{
				fname:   "fname",
				line:    4,
				comment: "#. comment2\n",
			},
		},
	}
	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#. comment1
#. comment2
#: fname:2 fname:4
msgid   "foo"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestWriteOutputNoComment(c *C) {
	msgIDs = map[string][]msgID{
		"foo": {
			{
				fname: "fname",
				line:  2,
			},
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

func (s *xgettextTestSuite) TestWriteOutputNoLocation(c *C) {
	msgIDs = map[string][]msgID{
		"foo": {
			{
				fname: "fname",
				line:  2,
			},
		},
	}

	opts.NoLocation = true
	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
msgid   "foo"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestWriteOutputFormatHint(c *C) {
	msgIDs = map[string][]msgID{
		"foo": {
			{
				fname:      "fname",
				line:       2,
				formatHint: "c-format",
			},
		},
	}

	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#: fname:2
#, c-format
msgid   "foo"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestWriteOutputPlural(c *C) {
	msgIDs = map[string][]msgID{
		"foo": {
			{
				msgidPlural: "plural",
				fname:       "fname",
				line:        2,
			},
		},
	}

	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#: fname:2
msgid   "foo"
msgid_plural   "plural"
msgstr[0]  ""
msgstr[1]  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestWriteOutputSorted(c *C) {
	msgIDs = map[string][]msgID{
		"aaa": {
			{
				fname: "fname",
				line:  2,
			},
		},
		"zzz": {
			{
				fname: "fname",
				line:  2,
			},
		},
	}

	opts.SortOutput = true
	// we need to run this a bunch of times as the ordering might
	// be right by pure chance
	for i := 0; i < 10; i++ {
		out := bytes.NewBuffer([]byte(""))
		writePotFile(out)

		expected := fmt.Sprintf(`%s
#: fname:2
msgid   "aaa"
msgstr  ""

#: fname:2
msgid   "zzz"
msgstr  ""

`, header)
		c.Assert(out.String(), Equals, expected)
	}
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

	// a real integration test :)
	outName := filepath.Join(c.MkDir(), "snappy.pot")
	os.Args = []string{"test-binary",
		"--output", outName,
		"--keyword", "i18n.G",
		"--keyword-plural", "i18n.NG",
		"--msgid-bugs-address", "snappy-devel@lists.ubuntu.com",
		"--package-name", "snappy",
		fname,
	}
	main()

	// verify its what we expect
	c.Assert(outName, testutil.FileEquals, fmt.Sprintf(`%s
#: %[2]s:9
msgid   "abc"
msgstr  ""

#. TRANSLATORS: foo comment
#. with multiple lines
#: %[2]s:6
msgid   "foo"
msgstr  ""

#. TRANSLATORS: plural
#: %[2]s:12
msgid   "singular"
msgid_plural   "plural"
msgstr[0]  ""
msgstr[1]  ""

#: %[2]s:14
#, c-format
msgid   "zz %%s"
msgstr  ""

`, header, fname))
}

func (s *xgettextTestSuite) TestProcessFilesConcat(c *C) {
	fname := makeGoSourceFile(c, []byte(`package main

func main() {
    // TRANSLATORS: foo comment
    i18n.G("foo\n" + "bar\n" + "baz")
}
`))
	err := processFiles([]string{fname})
	c.Assert(err, IsNil)

	c.Assert(msgIDs, DeepEquals, map[string][]msgID{
		"foo\\nbar\\nbaz": {
			{
				comment: "#. TRANSLATORS: foo comment\n",
				fname:   fname,
				line:    5,
			},
		},
	})
}

func (s *xgettextTestSuite) TestProcessFilesWithQuote(c *C) {
	fname := makeGoSourceFile(c, []byte(fmt.Sprintf(`package main

func main() {
    i18n.G(%[1]s foo "bar"%[1]s)
}
`, "`")))
	err := processFiles([]string{fname})
	c.Assert(err, IsNil)

	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#: %[2]s:4
msgid   " foo \"bar\""
msgstr  ""

`, header, fname)
	c.Check(out.String(), Equals, expected)

}

func (s *xgettextTestSuite) TestWriteOutputMultilines(c *C) {
	msgIDs = map[string][]msgID{
		"foo\\nbar\\nbaz": {
			{
				fname:   "fname",
				line:    2,
				comment: "#. foo\n",
			},
		},
	}
	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)
	expected := fmt.Sprintf(`%s
#. foo
#: fname:2
msgid   "foo\n"
        "bar\n"
        "baz"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestWriteOutputTidy(c *C) {
	msgIDs = map[string][]msgID{
		"foo\\nbar\\nbaz": {
			{
				fname: "fname",
				line:  2,
			},
		},
		"zzz\\n": {
			{
				fname: "fname",
				line:  4,
			},
		},
	}
	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)
	expected := fmt.Sprintf(`%s
#: fname:2
msgid   "foo\n"
        "bar\n"
        "baz"
msgstr  ""

#: fname:4
msgid   "zzz\n"
msgstr  ""

`, header)
	c.Assert(out.String(), Equals, expected)
}

func (s *xgettextTestSuite) TestProcessFilesWithDoubleQuote(c *C) {
	fname := makeGoSourceFile(c, []byte(`package main

func main() {
    i18n.G("foo \"bar\"")
}
`))
	err := processFiles([]string{fname})
	c.Assert(err, IsNil)

	out := bytes.NewBuffer([]byte(""))
	writePotFile(out)

	expected := fmt.Sprintf(`%s
#: %[2]s:4
msgid   "foo \"bar\""
msgstr  ""

`, header, fname)
	c.Check(out.String(), Equals, expected)

}
