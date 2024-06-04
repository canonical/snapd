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

package i18n

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var mockLocalePo = []byte(`
msgid ""
msgstr ""
"Project-Id-Version: snappy-test\n"
"Report-Msgid-Bugs-To: snappy-devel@lists.ubuntu.com\n"
"POT-Creation-Date: 2015-06-16 09:08+0200\n"
"Language: en_DK\n"
"MIME-Version: 1.0\n"
"Content-Type: text/plain; charset=UTF-8\n"
"Content-Transfer-Encoding: 8bit\n"
"Plural-Forms: nplurals=2; plural=n != 1;>\n"

msgid "plural_1"
msgid_plural "plural_2"
msgstr[0] "translated plural_1"
msgstr[1] "translated plural_2"

msgid "singular"
msgstr "translated singular"
`)

func makeMockTranslations(c *C, localeDir string) {
	fullLocaleDir := filepath.Join(localeDir, "en_DK", "LC_MESSAGES")
	err := os.MkdirAll(fullLocaleDir, 0755)
	c.Assert(err, IsNil)

	po := filepath.Join(fullLocaleDir, "snappy-test.po")
	mo := filepath.Join(fullLocaleDir, "snappy-test.mo")
	err = os.WriteFile(po, mockLocalePo, 0644)
	c.Assert(err, IsNil)

	cmd := exec.Command("msgfmt", po, "--output-file", mo)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	c.Assert(err, IsNil)
}

type i18nTestSuite struct {
	origLang       string
	origLcMessages string
}

var _ = Suite(&i18nTestSuite{})

func (s *i18nTestSuite) SetUpTest(c *C) {
	// this dir contains a special hand-crafted en_DK/snappy-test.mo
	// file
	localeDir := c.MkDir()
	makeMockTranslations(c, localeDir)

	// we use a custom test mo file
	TEXTDOMAIN = "snappy-test"

	s.origLang = os.Getenv("LANG")
	s.origLcMessages = os.Getenv("LC_MESSAGES")

	bindTextDomain("snappy-test", localeDir)
	os.Setenv("LANG", "en_DK.UTF-8")
	setLocale("")
}

func (s *i18nTestSuite) TearDownTest(c *C) {
	os.Setenv("LANG", s.origLang)
	os.Setenv("LC_MESSAGES", s.origLcMessages)
}

func (s *i18nTestSuite) TestTranslatedSingular(c *C) {
	// no G() to avoid adding the test string to snappy-pot
	var Gtest = G
	c.Assert(Gtest("singular"), Equals, "translated singular")
}

func (s *i18nTestSuite) TestTranslatesPlural(c *C) {
	// no NG() to avoid adding the test string to snappy-pot
	var NGtest = NG
	c.Assert(NGtest("plural_1", "plural_2", 1), Equals, "translated plural_1")
}

func (s *i18nTestSuite) TestTranslatedMissingLangNoCrash(c *C) {
	setLocale("invalid")

	// no G() to avoid adding the test string to snappy-pot
	var Gtest = G
	c.Assert(Gtest("singular"), Equals, "singular")
}

func (s *i18nTestSuite) TestInvalidTextDomainDir(c *C) {
	bindTextDomain("snappy-test", "/random/not/existing/dir")
	setLocale("invalid")

	// no G() to avoid adding the test string to snappy-pot
	var Gtest = G
	c.Assert(Gtest("singular"), Equals, "singular")
}

func (s *i18nTestSuite) TestLangpackResolverFromLangpack(c *C) {
	root := c.MkDir()
	localeDir := filepath.Join(root, "/usr/share/locale")
	err := os.MkdirAll(localeDir, 0755)
	c.Assert(err, IsNil)

	d := filepath.Join(root, "/usr/share/locale-langpack")
	makeMockTranslations(c, d)
	bindTextDomain("snappy-test", localeDir)
	setLocale("")

	// no G() to avoid adding the test string to snappy-pot
	var Gtest = G
	c.Assert(Gtest("singular"), Equals, "translated singular", Commentf("test with %q failed", d))
}

func (s *i18nTestSuite) TestLangpackResolverFromCore(c *C) {
	origSnapMountDir := dirs.SnapMountDir
	defer func() { dirs.SnapMountDir = origSnapMountDir }()
	dirs.SnapMountDir = c.MkDir()

	d := filepath.Join(dirs.SnapMountDir, "/core/current/usr/share/locale")
	makeMockTranslations(c, d)
	bindTextDomain("snappy-test", "/usr/share/locale")
	setLocale("")

	// no G() to avoid adding the test string to snappy-pot
	var Gtest = G
	c.Assert(Gtest("singular"), Equals, "translated singular", Commentf("test with %q failed", d))
}
