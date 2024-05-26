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

package builtin_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type personalFilesInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&personalFilesInterfaceSuite{
	iface: builtin.MustInterface("personal-files"),
})

func (s *personalFilesInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 personal-files:
  read: [$HOME/.read-dir, $HOME/.read-file, $HOME/.local/share/target]
  write: [$HOME/.write-dir, $HOME/.write-file, $HOME/.local/share/target, $HOME/.local/share/dir1/dir2/target]
apps:
 app:
  command: foo
  plugs: [personal-files]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "personal-files",
		Interface: "personal-files",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["personal-files"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *personalFilesInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "personal-files")
}

func (s *personalFilesInterfaceSuite) TestConnectedPlugAppArmorHappy(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), Equals, `
# Description: Can access specific personal files or directories in the 
# users's home directory.
# This is restricted because it gives file access to arbitrary locations.
owner "@{HOME}/.read-dir{,/,/**}" rk,
owner "@{HOME}/.read-file{,/,/**}" rk,
owner "@{HOME}/.local/share/target{,/,/**}" rk,
owner "@{HOME}/.write-dir{,/,/**}" rwkl,
owner "@{HOME}/.write-file{,/,/**}" rwkl,
owner "@{HOME}/.local/share/target{,/,/**}" rwkl,
owner "@{HOME}/.local/share/dir1/dir2/target{,/,/**}" rwkl,
`)

	c.Check("\n"+strings.Join(apparmorSpec.UpdateNS(), "\n"), Equals, `
  # Allow the personal-files interface to create potentially missing directories
  owner @{HOME}/ rw,
  owner @{HOME}/.local/ rw,
  owner @{HOME}/.local/share/ rw,
  owner @{HOME}/.local/share/dir1/ rw,
  owner @{HOME}/.local/share/dir1/dir2/ rw,`)
}

func (s *personalFilesInterfaceSuite) TestConnectedPlugApparmorErrorNotString(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 personal-files:
  read: [$HOME/.read-dir, $HOME/.read-file, $HOME/.local/share/target]
  write: [123]
apps:
 app:
  command: foo
  plugs: [personal-files]
`
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	plugInfo := plugSnap.Plugs["personal-files"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot))
	c.Assert(err, ErrorMatches, `cannot connect plug personal-files: 123 \(int64\) is not a string`)
}

func (s *personalFilesInterfaceSuite) TestConnectedPlugApparmorErrorNotStartWithHome(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 personal-files:
  read: [$HOME/.read-dir, $HOME/.read-file, $HOME/.local/share/target]
  write: [$NOTHOME/.local/share/target]
apps:
 app:
  command: foo
  plugs: [personal-files]
`
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	plugInfo := plugSnap.Plugs["personal-files"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot))
	c.Assert(err, ErrorMatches, `cannot connect plug personal-files: "\$NOTHOME/.local/share/target" must start with "\$HOME/"`)
}

func (s *personalFilesInterfaceSuite) TestConnectedPlugMountHappy(c *C) {
	mountSpec := &mount.Specification{}
	mylog.Check(mountSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(mountSpec.MountEntries(), HasLen, 0)
	c.Assert(mountSpec.UserMountEntries(), HasLen, 2)

	expectedUserMountEntries := []osutil.MountEntry{
		{
			Dir:     "$HOME/.local/share",
			Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"},
		},
		{
			Dir:     "$HOME/.local/share/dir1/dir2",
			Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"},
		},
	}
	c.Assert(mountSpec.UserMountEntries(), DeepEquals, expectedUserMountEntries)
}

func (s *personalFilesInterfaceSuite) TestConnectedPlugMountErrorNotStartWithHome(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 personal-files:
  read: [$HOME/.read-dir, $HOME/.read-file, $HOME/.local/share/target]
  write: [$NOTHOME/.local/share/target]
apps:
 app:
  command: foo
  plugs: [personal-files]
`
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	plugInfo := plugSnap.Plugs["personal-files"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	mountSpec := &mount.Specification{}
	mylog.Check(mountSpec.AddConnectedPlug(s.iface, plug, s.slot))
	c.Assert(err, ErrorMatches, `cannot connect plug personal-files: "\$NOTHOME/.local/share/target" must start with "\$HOME/"`)
}

func (s *personalFilesInterfaceSuite) TestConnectedPlugMountErrorAddUserEnsureDirsUnhappy(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 personal-files:
  read: [$HOME/.read-dir, $HOME/.read-file, $HOME/.local/share/target]
  write: [$HOME/.local/share/target]
apps:
 app:
  command: foo
  plugs: [personal-files]
`
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	plugInfo := plugSnap.Plugs["personal-files"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	mountSpec := &mount.Specification{}
	restore := builtin.MockDirsToEnsure(func(paths []string) ([]*interfaces.EnsureDirSpec, error) {
		return []*interfaces.EnsureDirSpec{
			{MustExistDir: "dir", EnsureDir: "dir/dir2"},
		}, nil
	})
	defer restore()
	mylog.Check(mountSpec.AddConnectedPlug(s.iface, plug, s.slot))
	c.Assert(err, ErrorMatches, `cannot connect plug personal-files: internal error: cannot use ensure-dir mount specification: directory that must exist "dir" is not an absolute path`)
}

func (s *personalFilesInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *personalFilesInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *personalFilesInterfaceSuite) TestSanitizePlugHappy(c *C) {
	const mockSnapYaml = `name: personal-files-plug-snap
version: 1.0
plugs:
 personal-files:
  read: ["$HOME/file1", "$HOME/.hidden1"]
  write: ["$HOME/dir1", "$HOME/.hidden2"]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["personal-files"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *personalFilesInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	const mockSnapYaml = `name: personal-files-plug-snap
version: 1.0
plugs:
 personal-files:
  $t
`
	errPrefix := `cannot add personal-files plug: `
	testCases := []struct {
		inp    string
		errStr string
	}{
		{`read: ""`, `"read" must be a list of strings`},
		{`read: [ 123 ]`, `"read" must be a list of strings`},
		{`read: [ "$HOME/foo/./bar" ]`, `cannot use "\$HOME/foo/./bar": try "\$HOME/foo/bar"`},
		{`read: [ "../foo" ]`, `"../foo" must start with "\$HOME/"`},
		{`read: [ "/foo[" ]`, `"/foo\[" contains a reserved apparmor char from .*`},
		{`write: ""`, `"write" must be a list of strings`},
		{`write: bar`, `"write" must be a list of strings`},
		{`read: [ "~/foo" ]`, `"~/foo" cannot contain "~"`},
		{`read: [ "$HOME/foo/~/foo" ]`, `"\$HOME/foo/~/foo" cannot contain "~"`},
		{`read: [ "$HOME/foo/../foo" ]`, `cannot use "\$HOME/foo/../foo": try "\$HOME/foo"`},
		{`read: [ "$HOME/home/$HOME/foo" ]`, `\$HOME must only be used at the start of the path of "\$HOME/home/\$HOME/foo"`},
		{`read: [ "$HOME/sweet/$HOME" ]`, `\$HOME must only be used at the start of the path of "\$HOME/sweet/\$HOME"`},
		{`read: [ "/@{FOO}" ]`, `"/@{FOO}" contains a reserved apparmor char from .*`},
		{`read: [ "/home/@{HOME}/foo" ]`, `"/home/@{HOME}/foo" contains a reserved apparmor char from .*`},
		{`read: [ "${HOME}/foo" ]`, `"\${HOME}/foo" contains a reserved apparmor char from .*`},
		{`read: [ "$HOME" ]`, `"\$HOME" must start with "\$HOME/"`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.inp, -1)
		info := snaptest.MockInfo(c, yml, nil)
		plug := info.Plugs["personal-files"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, errPrefix+t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *personalFilesInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
