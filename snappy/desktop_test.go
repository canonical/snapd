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

package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
)

var desktopAppYaml = []byte(`
name: foo
version: 1.0
`)

var mockDesktopFile = []byte(`
[Desktop Entry]
Name=foo
Icon=${SNAP}/foo.png`)

func (s *SnapTestSuite) TestAddPackageDesktopFiles(c *C) {
	expectedDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, false)

	m, err := parseSnapYamlData(desktopAppYaml, false)
	c.Assert(err, IsNil)

	// generate .desktop file in the package baseDir
	baseDir := c.MkDir()
	err = os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.desktop"), mockDesktopFile, 0644)
	c.Assert(err, IsNil)

	err = addPackageDesktopFiles(m, baseDir)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, true)
}

func (s *SnapTestSuite) TestRemovePackageDesktopFiles(c *C) {
	mockDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")

	err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockDesktopFilePath, mockDesktopFile, 0644)
	c.Assert(err, IsNil)
	m, err := parseSnapYamlData(desktopAppYaml, false)
	c.Assert(err, IsNil)

	err = removePackageDesktopFiles(m)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, false)
}

func (s *SnapTestSuite) TestDesktopFileIsAddedAndRemoved(c *C) {
	yamlFile, err := makeInstalledMockSnap(s.tempdir, string(desktopAppYaml))
	c.Assert(err, IsNil)
	part, err := NewInstalledSnap(yamlFile, testDeveloper)
	c.Assert(err, IsNil)

	// create a mock desktop file
	err = os.MkdirAll(filepath.Join(filepath.Dir(yamlFile), "gui"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(filepath.Dir(yamlFile), "gui", "foobar.desktop"), []byte(mockDesktopFile), 0644)
	c.Assert(err, IsNil)

	// ensure that activate creates the desktop file
	err = part.activate(false, nil)
	c.Assert(err, IsNil)

	mockDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	content, err := ioutil.ReadFile(mockDesktopFilePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, fmt.Sprintf(`
[Desktop Entry]
Name=foo
Icon=/snaps/foo.%s/1.0/foo.png`, testDeveloper))

	// deactivate removes it again
	err = part.deactivate(false, nil)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, false)
}

func (s *SnapTestSuite) TestDesktopFileSanitizeIgnoreNotWhitelisted(c *C) {
	m := &snapYaml{}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
UnknownKey=baz
nonsense
Icon=${SNAP}/meep

# the empty line above is fine`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo
Icon=/my/basedir/meep

# the empty line above is fine`)
}

func (s *SnapTestSuite) TestDesktopFileSanitizeFiltersExec(c *C) {
	m, err := parseSnapYamlData([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`), false)
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=baz
`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo`)
}

func (s *SnapTestSuite) TestDesktopFileSanitizeFiltersExecPrefix(c *C) {
	m, err := parseSnapYamlData([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`), false)
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app.evil.evil
`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo`)
}

func (s *SnapTestSuite) TestDesktopFileSanitizeFiltersExecOk(c *C) {
	m, err := parseSnapYamlData([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`), false)
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app %U
`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo
Exec=/snaps/bin/snap.app %U`)
}

// we do not support TryExec (even if its a valid line), this test ensures
// we do not accidentally enable it
func (s *SnapTestSuite) TestDesktopFileSanitizeFiltersTryExecIgnored(c *C) {
	m, err := parseSnapYamlData([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`), false)
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
TryExec=snap.app %U
`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo`)
}

func (s *SnapTestSuite) TestDesktopFileSanitizeWorthWithI18n(c *C) {
	m := &snapYaml{}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
GenericName=bar
GenericName[de]=einsehrlangeszusammengesetzteswort
GenericName[tlh_TLH]=Qapla'
GenericName[ca@valencia]=Hola!
Invalid=key
Invalid[i18n]=key
`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo
GenericName=bar
GenericName[de]=einsehrlangeszusammengesetzteswort
GenericName[tlh_TLH]=Qapla'
GenericName[ca@valencia]=Hola!`)
}

func (s *SnapTestSuite) TestDesktopFileRewriteExecLineInvalid(c *C) {
	m := &snapYaml{}
	_, err := rewriteExecLine(m, "Exec=invalid")
	c.Assert(err, ErrorMatches, `invalid exec command: "invalid"`)
}

func (s *SnapTestSuite) TestDesktopFileRewriteExecLineOk(c *C) {
	m, err := parseSnapYamlData([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`), false)
	c.Assert(err, IsNil)

	newl, err := rewriteExecLine(m, "Exec=snap.app")
	c.Assert(err, IsNil)
	c.Assert(newl, Equals, "Exec=/snaps/bin/snap.app")
}

func (s *SnapTestSuite) TestDesktopFileSanitizeDesktopActionsOk(c *C) {
	m := &snapYaml{}
	desktopContent := []byte(`[Desktop Action is-ok]`)

	e := sanitizeDesktopFile(m, "/my/basedir", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Action is-ok]`)
}

func (s *SnapTestSuite) TestDesktopFileTrimLang(c *C) {
	langs := []struct {
		in  string
		out string
	}{
		// langCodes
		{"[lang_COUNTRY@MODIFIER]=foo", "=foo"},
		{"[lang_COUNTRY]=bar", "=bar"},
		{"[lang_COUNTRY]=baz", "=baz"},
		{"[lang]=foobar", "=foobar"},
		// non-langCodes, should be ignored
		{"", ""},
		{"Name=foobar", "Name=foobar"},
	}
	for _, t := range langs {
		c.Assert(trimLang(t.in), Equals, t.out)
	}
}
