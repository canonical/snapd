// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snap_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/testutil"
)

type FileSuite struct{}

var _ = Suite(&FileSuite{})

type validateSuite struct {
	testutil.BaseTest
}

var _ = Suite(&validateSuite{})

func discard(string, ...interface{}) {}

func (s *validateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *validateSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *validateSuite) TestValidateContainerReallyEmptyFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	// the snap dir is a 0700 directory with nothing in it

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(snapdir.New(d), info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)

	err = snap.ValidateComponentContainer(snapdir.New(d), "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerEmptyButBadPermFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()

	stat, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(stat.Mode().Perm(), Equals, os.FileMode(0700)) // just to be sure

	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "meta", "snap.yaml"), nil, 0444), IsNil)

	// snapdir has /meta/snap.yaml, but / is 0700

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(snapdir.New(d), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateComponentContainerEmptyButBadPermFails(c *C) {
	d := c.MkDir()

	stat, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(stat.Mode().Perm(), Equals, os.FileMode(0700)) // just to be sure

	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "meta", "component.yaml"), nil, 0444), IsNil)

	// snapdir has /meta/component.yaml, but / is 0700

	err = snap.ValidateComponentContainer(snapdir.New(d), "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerMissingSnapYamlFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK), but no /meta/snap.yaml

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(snapdir.New(d), info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)

	// component's / and /meta are 0755 (i.e. OK), but no /meta/component.yaml

	err = snap.ValidateComponentContainer(snapdir.New(d), "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerSnapYamlBadPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "meta", "snap.yaml"), nil, 0), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, but isn't readable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(snapdir.New(d), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateComponentContainerSnapYamlBadPermsFails(c *C) {
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "meta", "component.yaml"), nil, 0), IsNil)

	// components's / and /meta are 0755 (i.e. OK),
	// /meta/component.yaml exists, but isn't readable

	err := snap.ValidateComponentContainer(snapdir.New(d), "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerSnapYamlNonRegularFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(syscall.Mkfifo(filepath.Join(d, "meta", "snap.yaml"), 0444), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, is readable, but isn't a file

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(snapdir.New(d), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

// emptyContainer returns a minimal container that passes
// ValidateContainer: / and /meta exist and are 0755, and
// /meta/snap.yaml is a regular world-readable file.
func emptyContainer(c *C) *snapdir.SnapDir {
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "meta", "snap.yaml"), nil, 0444), IsNil)
	return snapdir.New(d)
}

func (s *validateSuite) TestValidateContainerMinimalOKPermWorks(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := emptyContainer(c)
	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, is readable regular file
	// (this could be considered a test of emptyContainer)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateContainerMissingAppsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	// snapdir is empty: no apps

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerBadAppPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "foo"), nil, 0444), IsNil)

	// snapdir contains the app, but the app is not executable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerBadAppDirPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: apps/foo
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "apps"), 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "apps", "foo"), nil, 0555), IsNil)

	// snapdir contains executable app, but path to executable isn't rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerBadSvcPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 bar:
  command: svcs/bar
  daemon: simple
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "svcs"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "svcs", "bar"), nil, 0), IsNil)

	// snapdir contains service, but it isn't executable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerCompleterFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: cmds/foo
  completer: comp/foo.sh
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "cmds"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "cmds", "foo"), nil, 0555), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "comp"), 0755), IsNil)

	// snapdir contains executable app, in a rx path, but refers
	// to a completer that doesn't exist

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerBadAppPathOK(c *C) {
	// we actually support this, but don't validate it here
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: ../../../bin/echo
`
	d := emptyContainer(c)

	// snapdir does not contain the app, but the command is
	// "outside" so it might be OK

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateContainerSymlinksFails(c *C) {
	c.Skip("checking symlink targets not implemented yet")
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	fn := filepath.Join(d.Path(), "foo")
	c.Assert(os.WriteFile(fn+".real", nil, 0444), IsNil)
	c.Assert(os.Symlink(fn+".real", fn), IsNil)

	// snapdir contains a command that's a symlink to a file that's not world-rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerSymlinksOK(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	fn := filepath.Join(d.Path(), "foo")
	c.Assert(os.WriteFile(fn+".real", nil, 0555), IsNil)
	c.Assert(os.Symlink(fn+".real", fn), IsNil)

	// snapdir contains a command that's a symlink to a file that's world-rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateContainerAppsOK(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: cmds/foo
  completer: comp/foo.sh
 bar:
  command: svcs/bar
  daemon: simple
 baz:
  command: cmds/foo --with=baz
 quux:
  command: cmds/foo
  daemon: simple
 meep:
  command: comp/foo.sh
  daemon: simple
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "cmds"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "cmds", "foo"), nil, 0555), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "comp"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "comp", "foo.sh"), nil, 0555), IsNil)

	c.Assert(os.Mkdir(filepath.Join(d.Path(), "svcs"), 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d.Path(), "svcs", "bar"), nil, 0500), IsNil)

	c.Assert(os.Mkdir(filepath.Join(d.Path(), "garbage"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "garbage", "zero"), 0), IsNil)
	defer os.Chmod(filepath.Join(d.Path(), "garbage", "zero"), 0755)

	// snapdir contains:
	//  * a command that's world-rx, and its directory is
	//    world-rx, and its completer is world-r in a world-rx
	//    directory
	// * a service that's root-executable, and its directory is
	//   not readable nor searchable - and that's OK! (NOTE as
	//   this test should pass as non-rooot, the directory is 0700
	//   instead of 0000)
	// * a command with arguments
	// * a service that is also a command
	// * a service that is also a completer (WAT)
	// * an extra directory only root can look at (this would fail
	//   if not running the suite as root, and SkipDir didn't
	//   work)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(d, info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateComponentContainer(c *C) {
	const yaml = `component: empty-snap+test-comp
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "meta", "component.yaml"), []byte(yaml), 0444), IsNil)

	err := snap.ValidateComponentContainer(snapdir.New(d), "empty-snap+comp.comp", discard)
	c.Check(err, IsNil)
}
