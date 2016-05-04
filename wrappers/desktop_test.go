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

package wrappers_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snaptest"
	"github.com/ubuntu-core/snappy/wrappers"
)

type desktopSuite struct {
	tempdir string
}

var _ = Suite(&desktopSuite{})

func (s *desktopSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *desktopSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

var desktopAppYaml = `
name: foo
version: 1.0
`

var mockDesktopFile = []byte(`
[Desktop Entry]
Name=foo
Icon=${SNAP}/foo.png`)

func (s *desktopSuite) TestAddPackageDesktopFiles(c *C) {
	expectedDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, false)

	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: 11})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.desktop"), mockDesktopFile, 0644)
	c.Assert(err, IsNil)

	err = wrappers.AddSnapDesktopFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, true)
}

func (s *desktopSuite) TestRemovePackageDesktopFiles(c *C) {
	mockDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")

	err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockDesktopFilePath, mockDesktopFile, 0644)
	c.Assert(err, IsNil)
	info, err := snap.InfoFromSnapYaml([]byte(desktopAppYaml))
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapDesktopFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, false)
}

// sanitize

type sanitizeDesktopFileSuite struct{}

var _ = Suite(&sanitizeDesktopFileSuite{})

func (s *sanitizeDesktopFileSuite) TestSanitizeIgnoreNotWhitelisted(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{OfficialName: "foo", Revision: 12}}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
UnknownKey=baz
nonsense
Icon=${SNAP}/meep

# the empty line above is fine`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo
Icon=/snap/foo/12/meep

# the empty line above is fine`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExec(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=baz
`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExecPrefix(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app.evil.evil
`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExecOk(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app %U
`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo
Exec=/snap/bin/snap.app %U`)
}

// we do not support TryExec (even if its a valid line), this test ensures
// we do not accidentally enable it
func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersTryExecIgnored(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
TryExec=snap.app %U
`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeWorthWithI18n(c *C) {
	snap := &snap.Info{}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
GenericName=bar
GenericName[de]=einsehrlangeszusammengesetzteswort
GenericName[tlh_TLH]=Qapla'
GenericName[ca@valencia]=Hola!
Invalid=key
Invalid[i18n]=key
`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
Name=foo
GenericName=bar
GenericName[de]=einsehrlangeszusammengesetzteswort
GenericName[tlh_TLH]=Qapla'
GenericName[ca@valencia]=Hola!`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeDesktopActionsOk(c *C) {
	snap := &snap.Info{}
	desktopContent := []byte(`[Desktop Action is-ok]`)

	e := wrappers.SanitizeDesktopFile(snap, desktopContent)
	c.Assert(string(e), Equals, `[Desktop Action is-ok]`)
}

func (s *sanitizeDesktopFileSuite) TestRewriteExecLineInvalid(c *C) {
	snap := &snap.Info{}
	_, err := wrappers.RewriteExecLine(snap, "Exec=invalid")
	c.Assert(err, ErrorMatches, `invalid exec command: "invalid"`)
}

func (s *sanitizeDesktopFileSuite) TestRewriteExecLineOk(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)

	newl, err := wrappers.RewriteExecLine(snap, "Exec=snap.app")
	c.Assert(err, IsNil)
	c.Assert(newl, Equals, "Exec=/snap/bin/snap.app")
}

func (s *sanitizeDesktopFileSuite) TestTrimLang(c *C) {
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
		// corner case
		{"[foo=bar", "[foo=bar"},
	}
	for _, t := range langs {
		c.Assert(wrappers.TrimLang(t.in), Equals, t.out)
	}
}
