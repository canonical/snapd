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

package userd_test

import (
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/userd"
)

type autostartSuite struct {
	dir                string
	autostartDir       string
	userDir            string
	userCurrentRestore func()
}

var _ = Suite(&autostartSuite{})

func (s *autostartSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
	snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	s.userDir = path.Join(s.dir, "home")
	s.autostartDir = path.Join(s.userDir, ".config", "autostart")
	s.userCurrentRestore = userd.MockUserCurrent(func() (*user.User, error) {
		return &user.User{HomeDir: s.userDir}, nil
	})

	err := os.MkdirAll(s.autostartDir, 0755)
	c.Assert(err, IsNil)
}

func (s *autostartSuite) TearDownTest(c *C) {
	s.dir = c.MkDir()
	dirs.SetRootDir("/")
	if s.userCurrentRestore != nil {
		s.userCurrentRestore()
	}
}

func (s *autostartSuite) TestFindExec(c *C) {
	allGood := `[Desktop Entry]
Exec=foo --bar
`
	allGoodWithFlags := `[Desktop Entry]
Exec=foo --bar "%%p" %U %D +%s %%
`
	noExec := `[Desktop Entry]
Type=Application
`
	emptyExec := `[Desktop Entry]
Exec=
`
	onlySpacesExec := `[Desktop Entry]
Exec=
`
	for i, tc := range []struct {
		in  string
		out string
		err string
	}{{
		in:  allGood,
		out: "foo --bar",
	}, {
		in:  noExec,
		err: "Exec not found or invalid",
	}, {
		in:  emptyExec,
		err: "Exec not found or invalid",
	}, {
		in:  onlySpacesExec,
		err: "Exec not found or invalid",
	}, {
		in:  allGoodWithFlags,
		out: `foo --bar "%p"   + %`,
	}} {
		c.Logf("tc %d", i)

		cmd, err := userd.FindExec([]byte(tc.in))
		if tc.err != "" {
			c.Check(cmd, Equals, "")
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
			c.Check(cmd, Equals, tc.out)
		}
	}
}

var mockYaml = `name: snapname
version: 1.0
apps:
 foo:
  command: run-app
  autostart: foo-stable.desktop
`

func (s *autostartSuite) TestTryAutostartAppValid(c *C) {
	si := snaptest.MockSnapCurrent(c, mockYaml, &snap.SideInfo{Revision: snap.R("x2")})

	appWrapperPath := si.Apps["foo"].WrapperPath()
	err := os.MkdirAll(filepath.Dir(appWrapperPath), 0755)
	c.Assert(err, IsNil)

	appCmd := testutil.MockCommand(c, appWrapperPath, "")
	defer appCmd.Restore()

	fooDesktopFile := filepath.Join(s.autostartDir, "foo-stable.desktop")
	writeFile(c, fooDesktopFile,
		[]byte(`[Desktop Entry]
Exec=this-is-ignored -a -b --foo="a b c" -z "dev"
`))

	cmd, err := userd.AutostartCmd("snapname", fooDesktopFile)
	c.Assert(err, IsNil)
	c.Assert(cmd.Path, Equals, appWrapperPath)

	err = cmd.Start()
	c.Assert(err, IsNil)
	cmd.Wait()

	c.Assert(appCmd.Calls(), DeepEquals,
		[][]string{
			{
				filepath.Base(appWrapperPath),
				"-a",
				"-b",
				"--foo=a b c",
				"-z",
				"dev",
			},
		})
}

func (s *autostartSuite) TestTryAutostartAppNoMatchingApp(c *C) {
	snaptest.MockSnapCurrent(c, mockYaml, &snap.SideInfo{Revision: snap.R("x2")})

	fooDesktopFile := filepath.Join(s.autostartDir, "foo-no-match.desktop")
	writeFile(c, fooDesktopFile,
		[]byte(`[Desktop Entry]
Exec=this-is-ignored -a -b --foo="a b c" -z "dev"
`))

	cmd, err := userd.AutostartCmd("snapname", fooDesktopFile)
	c.Assert(cmd, IsNil)
	c.Assert(err, ErrorMatches, `cannot match desktop file with snap snapname applications`)
}

func (s *autostartSuite) TestTryAutostartAppNoSnap(c *C) {
	fooDesktopFile := filepath.Join(s.autostartDir, "foo-stable.desktop")
	writeFile(c, fooDesktopFile,
		[]byte(`[Desktop Entry]
Exec=this-is-ignored -a -b --foo="a b c" -z "dev"
`))

	cmd, err := userd.AutostartCmd("snapname", fooDesktopFile)
	c.Assert(cmd, IsNil)
	c.Assert(err, ErrorMatches, `cannot find current revision for snap snapname.*`)
}

func (s *autostartSuite) TestTryAutostartAppBadExec(c *C) {
	snaptest.MockSnapCurrent(c, mockYaml, &snap.SideInfo{Revision: snap.R("x2")})

	fooDesktopFile := filepath.Join(s.autostartDir, "foo-stable.desktop")
	writeFile(c, fooDesktopFile,
		[]byte(`[Desktop Entry]
Foo=bar
`))

	cmd, err := userd.AutostartCmd("snapname", fooDesktopFile)
	c.Assert(cmd, IsNil)
	c.Assert(err, ErrorMatches, `cannot determine startup command for application foo in snap snapname: Exec not found or invalid`)
}

func writeFile(c *C, path string, content []byte) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(path, content, 0644)
	c.Assert(err, IsNil)
}

func (s *autostartSuite) TestTryAutostartMany(c *C) {
	var mockYamlTemplate = `name: {snap}
version: 1.0
apps:
 foo:
  command: run-app
  autostart: foo-stable.desktop
`

	snaptest.MockSnapCurrent(c, strings.Replace(mockYamlTemplate, "{snap}", "a-foo", -1),
		&snap.SideInfo{Revision: snap.R("x2")})
	snaptest.MockSnapCurrent(c, strings.Replace(mockYamlTemplate, "{snap}", "b-foo", -1),
		&snap.SideInfo{Revision: snap.R("x2")})
	writeFile(c, filepath.Join(s.userDir, "snap/a-foo/current/.config/autostart/foo-stable.desktop"),
		[]byte(`[Desktop Entry]
Foo=bar
`))
	writeFile(c, filepath.Join(s.userDir, "snap/b-foo/current/.config/autostart/no-match.desktop"),
		[]byte(`[Desktop Entry]
Exec=no-snap
`))
	writeFile(c, filepath.Join(s.userDir, "snap/c-foo/current/.config/autostart/no-snap.desktop"),
		[]byte(`[Desktop Entry]
Exec=no-snap
`))

	err := userd.AutostartSessionApps()
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `- "foo-stable.desktop": cannot determine startup command for application foo in snap a-foo: Exec not found or invalid
- "no-match.desktop": cannot match desktop file with snap b-foo applications
- "no-snap.desktop": cannot find current revision for snap c-foo: readlink.*no such file or directory
`)
}
