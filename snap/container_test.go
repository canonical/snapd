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
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

type FileSuite struct{}

var _ = Suite(&FileSuite{})

type validateSuite struct {
	testutil.BaseTest

	snapDirPath      string
	snapSquashfsPath string
	containerType    string
}

type dirContainerValidateSuite struct{ validateSuite }
type squashfsContainerValidateSuite struct{ validateSuite }

var _ = Suite(&dirContainerValidateSuite{validateSuite{containerType: "dir"}})
var _ = Suite(&squashfsContainerValidateSuite{validateSuite{containerType: "squashfs"}})

func discard(string, ...interface{}) {}

func (s *validateSuite) container() snap.Container {
	if s.containerType == "squashfs" {
		snap := squashfs.New(s.snapSquashfsPath)
		if err := snap.Build(s.snapDirPath, nil); err != nil {
			panic(fmt.Sprintf("internal error: couldn't build snap: %s", err))
		}
		return snap
	}
	return snapdir.New(s.snapDirPath)
}

func (s *validateSuite) SetUpTest(c *C) {
	s.snapDirPath = c.MkDir()
	s.snapSquashfsPath = filepath.Join(c.MkDir(), "foo.snap")
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
	container := s.container()
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(container, info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)

	err = snap.ValidateComponentContainer(container, "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerEmptyButBadPermFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	stat, err := os.Stat(s.snapDirPath)
	c.Assert(err, IsNil)
	c.Check(stat.Mode().Perm(), Equals, os.FileMode(0700)) // just to be sure

	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "meta", "snap.yaml"), nil, 0444), IsNil)

	// snapdir has /meta/snap.yaml, but / is 0700

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateComponentContainerEmptyButBadPermFails(c *C) {
	stat, err := os.Stat(s.snapDirPath)
	c.Assert(err, IsNil)
	c.Check(stat.Mode().Perm(), Equals, os.FileMode(0700)) // just to be sure

	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "meta", "component.yaml"), nil, 0444), IsNil)

	// snapdir has /meta/component.yaml, but / is 0700

	err = snap.ValidateComponentContainer(s.container(), "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerMissingSnapYamlFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	container := s.container()
	c.Assert(os.Chmod(s.snapDirPath, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK), but no /meta/snap.yaml

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(container, info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)

	// component's / and /meta are 0755 (i.e. OK), but no /meta/component.yaml

	err = snap.ValidateComponentContainer(container, "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerSnapYamlBadPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	c.Assert(os.Chmod(s.snapDirPath, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "meta", "snap.yaml"), nil, 0), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, but isn't readable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateComponentContainerSnapYamlBadPermsFails(c *C) {
	c.Assert(os.Chmod(s.snapDirPath, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "meta", "component.yaml"), nil, 0), IsNil)

	// components's / and /meta are 0755 (i.e. OK),
	// /meta/component.yaml exists, but isn't readable

	err := snap.ValidateComponentContainer(s.container(), "empty-snap+comp.comp", discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerSnapYamlNonRegularFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	c.Assert(os.Chmod(s.snapDirPath, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	c.Assert(syscall.Mkfifo(filepath.Join(s.snapDirPath, "meta", "snap.yaml"), 0444), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, is readable, but isn't a file

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

// bootstrapEmptyContainer creates a minimal container directory under
// s.snapDirPath that passes ValidateContainer: / and /meta exist and
// are 0755, and /meta/snap.yaml is a regular world-readable file.
func (s *validateSuite) bootstrapEmptyContainer(c *C) {
	c.Assert(os.Chmod(s.snapDirPath, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "meta", "snap.yaml"), nil, 0444), IsNil)
}

func (s *validateSuite) TestValidateContainerMinimalOKPermWorks(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, is readable regular file
	// (this could be considered a test of emptyContainer)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateContainerMissingAppsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	s.bootstrapEmptyContainer(c)
	// snapdir is empty: no apps

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, Equals, snap.ErrMissingPaths)
}

func (s *validateSuite) TestValidateContainerBadAppPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "foo"), nil, 0444), IsNil)

	// snapdir contains the app, but the app is not executable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerBadAppDirPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: apps/foo
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "apps"), 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "apps", "foo"), nil, 0555), IsNil)

	// snapdir contains executable app, but path to executable isn't rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
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
	s.bootstrapEmptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "svcs"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "svcs", "bar"), nil, 0), IsNil)

	// snapdir contains service, but it isn't executable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
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
	s.bootstrapEmptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "cmds"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "cmds", "foo"), nil, 0555), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "comp"), 0755), IsNil)

	// snapdir contains executable app, in a rx path, but refers
	// to a completer that doesn't exist

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
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
	s.bootstrapEmptyContainer(c)

	// snapdir does not contain the app, but the command is
	// "outside" so it might be OK

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
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
	s.bootstrapEmptyContainer(c)
	fn := filepath.Join(s.snapDirPath, "foo")
	c.Assert(os.WriteFile(fn+".real", nil, 0444), IsNil)
	c.Assert(os.Symlink(fn+".real", fn), IsNil)

	// snapdir contains a command that's a symlink to a file that's not world-rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerSymlinksOK(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	s.bootstrapEmptyContainer(c)
	fn := filepath.Join(s.snapDirPath, "foo")
	c.Assert(os.WriteFile(fn+".real", nil, 0555), IsNil)
	c.Assert(os.Symlink(fn+".real", fn), IsNil)

	// snapdir contains a command that's a symlink to a file that's world-rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateContainerSymlinksMetaBadTargetMode(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	externalSymlink := filepath.Join(s.snapDirPath, "meta", "symlink")
	// target is has bad mode
	const mode = os.FileMode(0711)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "target"), nil, mode), IsNil)
	c.Assert(os.Symlink("../target", externalSymlink), IsNil)

	container := s.container()

	symlinkInfo, err := snap.EvalAndValidateSymlink(container, "meta/symlink")
	c.Check(err, IsNil)
	c.Check(symlinkInfo.Mode(), Equals, mode)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(container, info, discard)
	c.Check(err, ErrorMatches, "snap is unusable due to bad permissions")
}

func (s *validateSuite) TestValidateContainerSymlinksMetaBadTargetMode0000(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "meta"), 0755), IsNil)
	externalSymlink := filepath.Join(s.snapDirPath, "meta", "symlink")
	// target is has bad mode
	const mode = os.FileMode(0000)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "target"), nil, mode), IsNil)
	c.Assert(os.Symlink("../target", externalSymlink), IsNil)

	container := s.container()

	symlinkInfo, err := snap.EvalAndValidateSymlink(container, "meta/symlink")
	c.Check(err, IsNil)
	c.Check(symlinkInfo.Mode(), Equals, mode)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(container, info, discard)
	c.Check(err, ErrorMatches, "snap is unusable due to bad permissions")
}

func (s *validateSuite) TestValidateContainerMetaExternalAbsSymlinksFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "meta", "gui", "icons"), 0755), IsNil)
	externalSymlink := filepath.Join(s.snapDirPath, "meta", "gui", "icons", "snap.empty-snap.png")
	c.Assert(os.Symlink("/etc/shadow", externalSymlink), IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	mockLogf := func(format string, v ...interface{}) {
		msg := fmt.Sprintf(format, v...)
		c.Check(msg, Equals, "external symlink found: meta/gui/icons/snap.empty-snap.png -> /etc/shadow")
	}

	err = snap.ValidateSnapContainer(s.container(), info, mockLogf)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerMetaExternalRelativeSymlinksFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "meta", "gui", "icons"), 0755), IsNil)
	externalSymlink := filepath.Join(s.snapDirPath, "meta", "gui", "icons", "snap.empty-snap.png")
	// target is cleaned and checked if it escapes beyond path root folder
	c.Assert(os.Symlink("1/../../2/../../3/4/../../../../..", externalSymlink), IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	mockLogf := func(format string, v ...interface{}) {
		msg := fmt.Sprintf(format, v...)
		c.Check(msg, Equals, "external symlink found: meta/gui/icons/snap.empty-snap.png -> 1/../../2/../../3/4/../../../../..")
	}

	err = snap.ValidateSnapContainer(s.container(), info, mockLogf)
	c.Check(err, Equals, snap.ErrBadModes)
}

func (s *validateSuite) TestValidateContainerMetaExternalRelativeSymlinksOk(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "meta", "gui", "icons"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "target"), nil, 0644), IsNil)
	externalSymlink := filepath.Join(s.snapDirPath, "meta", "gui", "icons", "snap.empty-snap.png")
	// target is cleaned and checked if it escapes beyond path root folder
	c.Assert(os.Symlink("1/../2/../../3/4/../../../../target", externalSymlink), IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateContainerMetaDirectorySymlinksFail(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	c.Assert(os.Chmod(s.snapDirPath, 0755), IsNil)
	// no need to populate the symlink target with snap.yaml as the validator
	// will fail with ErrMissingPaths even it was added.
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "target"), 0755), IsNil)
	c.Assert(os.Symlink("target", filepath.Join(s.snapDirPath, "meta")), IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	metaDirSymlinkErrFound := false
	mockLogf := func(format string, v ...interface{}) {
		msg := fmt.Sprintf(format, v...)
		if msg == "meta directory cannot be a symlink" {
			metaDirSymlinkErrFound = true
		}
	}

	err = snap.ValidateSnapContainer(s.container(), info, mockLogf)
	c.Check(metaDirSymlinkErrFound, Equals, true)
	// the check for missing files precedes check for permission errors, so we
	// check for it instead.
	c.Check(err, Equals, snap.ErrMissingPaths)
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
	if s.containerType == "squashfs" {
		c.Skip("Cannot build snap squashfs with garbge/zero directory permissions")
	}
	s.bootstrapEmptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "cmds"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "cmds", "foo"), nil, 0555), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "comp"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "comp", "foo.sh"), nil, 0555), IsNil)

	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "svcs"), 0700), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "svcs", "bar"), nil, 0500), IsNil)

	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "garbage"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(s.snapDirPath, "garbage", "zero"), 0), IsNil)
	defer os.Chmod(filepath.Join(s.snapDirPath, "garbage", "zero"), 0755)

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

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateSymlinkLoop(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.Symlink("1", filepath.Join(s.snapDirPath, "meta", "2")), IsNil)
	c.Assert(os.Symlink("2", filepath.Join(s.snapDirPath, "meta", "3")), IsNil)
	c.Assert(os.Symlink("3", filepath.Join(s.snapDirPath, "meta", "1")), IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	loopFound := false
	mockLogf := func(format string, v ...interface{}) {
		msg := fmt.Sprintf(format, v...)
		if msg == "too many levels of symbolic links" {
			loopFound = true
		}
	}

	err = snap.ValidateSnapContainer(s.container(), info, mockLogf)
	c.Check(err, Equals, snap.ErrBadModes)
	c.Check(loopFound, Equals, true)
}

func (s *validateSuite) TestValidateSymlinkOk(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	s.bootstrapEmptyContainer(c)
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "media", "sub"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.snapDirPath, "media", "icon.png"), nil, 0644), IsNil)
	c.Assert(os.Symlink("../icon.png", filepath.Join(s.snapDirPath, "media", "sub", "symlink-1.png")), IsNil)
	c.Assert(os.Symlink("symlink-1.png", filepath.Join(s.snapDirPath, "media", "sub", "symlink-2.png")), IsNil)
	c.Assert(os.Symlink("../media/sub/symlink-2.png", filepath.Join(s.snapDirPath, "meta", "icon.png")), IsNil)
	// all symlinks outside meta directory are allowed
	c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, "bin"), 0755), IsNil)
	c.Assert(os.Symlink("/usr/bin/python3", filepath.Join(s.snapDirPath, "bin", "python3")), IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snap.ValidateSnapContainer(s.container(), info, discard)
	c.Check(err, IsNil)
}

func (s *validateSuite) TestValidateSymlinkExternal(c *C) {
	type testcase struct {
		path   string
		target string
	}

	s.bootstrapEmptyContainer(c)
	for _, t := range []testcase{
		{"meta/snap.yaml", "../.."},
		{"meta/snap.yaml", "../../"},
		{"meta/snap.yaml", "../../rev2"},
		{"meta/snap.yaml", "../../../core/current/meta/snap.yaml"},
		{"meta/gui/icons/snap.png", "../1/../../2/../../3/4/../../../test"},
		{"meta/gui/icons/snap.png", "/etc/shadow"},
		{"meta/gui/icons/snap.png", "/var/snap/other-snap/current/sensitive"},
	} {
		c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, filepath.Dir(t.path)), 0755), IsNil)
		c.Assert(os.RemoveAll(filepath.Join(s.snapDirPath, t.path)), IsNil)
		c.Assert(os.Symlink(t.target, filepath.Join(s.snapDirPath, t.path)), IsNil)

		cmt := Commentf(fmt.Sprintf("path: %s, target: %s", t.path, t.target))
		expectedError := fmt.Sprintf("external symlink found: %s -> %s", t.path, t.target)
		_, err := snap.EvalAndValidateSymlink(s.container(), t.path)
		c.Check(err, ErrorMatches, expectedError, cmt)
	}
}

func (s *validateSuite) TestValidateSymlinkSnapMount(c *C) {
	type testcase struct {
		path   string
		target string
	}

	s.bootstrapEmptyContainer(c)
	for _, t := range []testcase{
		{"meta/snap.yaml", ".."},
		{"meta/snap.yaml", "../"},
		{"meta/gui/icons/snap.png", "../1/../../2/../../3/4/../.."},
	} {
		c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, filepath.Dir(t.path)), 0755), IsNil)
		c.Assert(os.RemoveAll(filepath.Join(s.snapDirPath, t.path)), IsNil)
		c.Assert(os.Symlink(t.target, filepath.Join(s.snapDirPath, t.path)), IsNil)

		cmt := Commentf(fmt.Sprintf("path: %s, target: %s", t.path, t.target))
		expectedError := fmt.Sprintf("bad symlink found: %s -> %s", t.path, t.target)
		_, err := snap.EvalAndValidateSymlink(s.container(), t.path)
		c.Check(err, ErrorMatches, expectedError, cmt)
	}
}

func (s *validateSuite) TestValidateSymlinkMeta(c *C) {
	type testcase struct {
		path   string
		target string
	}

	s.bootstrapEmptyContainer(c)
	for _, t := range []testcase{
		{"meta/snap.yaml", "."},
		{"meta/snap.yaml", "../meta"},
		{"meta/gui/icons/snap.png", "../1/../../2/../../3/4/../../meta"},
	} {
		c.Assert(os.MkdirAll(filepath.Join(s.snapDirPath, filepath.Dir(t.path)), 0755), IsNil)
		c.Assert(os.RemoveAll(filepath.Join(s.snapDirPath, t.path)), IsNil)
		c.Assert(os.Symlink(t.target, filepath.Join(s.snapDirPath, t.path)), IsNil)

		cmt := Commentf(fmt.Sprintf("path: %s, target: %s", t.path, t.target))
		expectedError := fmt.Sprintf("bad symlink found: %s -> %s", t.path, t.target)
		_, err := snap.EvalAndValidateSymlink(s.container(), t.path)
		c.Check(err, ErrorMatches, expectedError, cmt)
	}
}

func (s *validateSuite) TestShouldValidateSymlink(c *C) {
	type testcase struct {
		path     string
		expected bool
	}

	for _, t := range []testcase{
		{"meta", true},
		{"meta/snap.yaml", true},
		{"meta/gui/icons/snap.png", true},
		{"meta/gui/snap.desktop", true},
		{"etc", false},
		{"etc/test", false},
		{"other", false},
	} {
		cmt := Commentf(fmt.Sprintf("path: %s, expected: %t", t.path, t.expected))
		c.Check(snap.ShouldValidateSymlink(t.path), Equals, t.expected, cmt)
	}
}
