// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package cgroup_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type freezerV1Suite struct{}

var _ = Suite(&freezerV1Suite{})

func (s *freezerV1Suite) TestFreezeSnapProcessesV1(c *C) {
	defer cgroup.MockVersion(cgroup.V1, nil)()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	n := "foo"                                                                 // snap name
	p := filepath.Join(cgroup.FreezerCgroupV1Dir(), fmt.Sprintf("snap.%s", n)) // snap freezer cgroup
	f := filepath.Join(p, "freezer.state")                                     // freezer.state file of the cgroup

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(cgroup.FreezeSnapProcesses(n), IsNil)
	_, err := os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the freezer cgroup filesystem exists but the particular cgroup
	// doesn't exist we don nothing at all.
	c.Assert(os.MkdirAll(cgroup.FreezerCgroupV1Dir(), 0755), IsNil)
	c.Assert(cgroup.FreezeSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the cgroup exists we write FROZEN the freezer.state file.
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(cgroup.FreezeSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	c.Assert(f, testutil.FileEquals, `FROZEN`)
}

func (s *freezerV1Suite) TestThawSnapProcessesV1(c *C) {
	defer cgroup.MockVersion(cgroup.V1, nil)()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	n := "foo"                                                                 // snap name
	p := filepath.Join(cgroup.FreezerCgroupV1Dir(), fmt.Sprintf("snap.%s", n)) // snap freezer cgroup
	f := filepath.Join(p, "freezer.state")                                     // freezer.state file of the cgroup

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(cgroup.ThawSnapProcesses(n), IsNil)
	_, err := os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the freezer cgroup filesystem exists but the particular cgroup
	// doesn't exist we don nothing at all.
	c.Assert(os.MkdirAll(cgroup.FreezerCgroupV1Dir(), 0755), IsNil)
	c.Assert(cgroup.ThawSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the cgroup exists we write THAWED the freezer.state file.
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(cgroup.ThawSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	c.Assert(f, testutil.FileEquals, `THAWED`)
}

type freezerV2Suite struct{}

var _ = Suite(&freezerV2Suite{})

func (s *freezerV2Suite) TestFreezeSnapProcessesV2OtherGroups(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// app started by root
	g1 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.app.1234-1234-1234.scope/cgroup.freeze")
	// service started by systemd
	g2 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.svc.service/cgroup.freeze")
	// user applications
	g3 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1234.slice/user@1234.service/snap.foo.user-app.1234-1234-1234.scope/cgroup.freeze")
	// user service
	g4 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1234.slice/user@1234.service/snap.foo.user-svc.service/cgroup.freeze")
	canary := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.canary.svc.service/cgroup.freeze")
	// a subgroup of the group of a snap
	canarySubgroup := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.svc.service/snap.foo.subgroup.scope/cgroup.freeze")

	pid := os.Getpid()

	// freezing needs to inspect our own cgroup, which will fail without
	// proper mocking
	err := cgroup.FreezeSnapProcesses("foo")
	c.Check(err, ErrorMatches, fmt.Sprintf("open %s/proc/%v/cgroup: no such file or directory", dirs.GlobalRootDir, pid))

	procPidCgroup := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%v/cgroup", pid))
	c.Assert(os.MkdirAll(filepath.Dir(procPidCgroup), 0755), IsNil)
	c.Assert(ioutil.WriteFile(procPidCgroup, []byte("0::/foo/bar"), 0755), IsNil)

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(cgroup.FreezeSnapProcesses("foo"), IsNil)

	for _, p := range []string{g1, g2, g3, g4, canary, canarySubgroup} {
		_, err := os.Stat(p)
		c.Assert(os.IsNotExist(err), Equals, true)
	}

	// prepare the stage
	for _, p := range []string{g1, g2, g3, g4, canary, canarySubgroup} {
		c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
		c.Assert(ioutil.WriteFile(p, []byte("0"), 0644), IsNil)
	}

	c.Assert(cgroup.FreezeSnapProcesses("foo"), IsNil)
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "1")
	}
	// canaries have not been changed
	c.Assert(canary, testutil.FileEquals, "0")
	c.Assert(canarySubgroup, testutil.FileEquals, "0")

	// all groups are 'frozen', repeating the action does not break anything
	c.Assert(cgroup.FreezeSnapProcesses("foo"), IsNil)
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "1")
	}
	// canaries have not been changed
	c.Assert(canary, testutil.FileEquals, "0")
	c.Assert(canarySubgroup, testutil.FileEquals, "0")

	// unfreeze some groups
	for _, p := range []string{g2, g3} {
		c.Assert(ioutil.WriteFile(p, []byte("0"), 0644), IsNil)
	}
	c.Assert(cgroup.FreezeSnapProcesses("foo"), IsNil)
	// all are frozen again
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "1")
	}
}

func (s *freezerV2Suite) TestFreezeSnapProcessesV2OwnGroup(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// our own cgroup
	gOwn := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.app.own-own-own.scope/cgroup.freeze")
	// app started by root
	g1 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.app.1234-1234-1234.scope/cgroup.freeze")
	// service started by systemd
	g2 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.svc.service/cgroup.freeze")
	// user applications
	g3 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1234.slice/user@1234.service/snap.foo.user-app.1234-1234-1234.scope/cgroup.freeze")
	// user service
	g4 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1234.slice/user@1234.service/snap.foo.user-svc.service/cgroup.freeze")
	canary := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.canary.svc.service/cgroup.freeze")
	// a subgroup of the group of a snap

	pid := os.Getpid()

	// freezing needs to inspect our own cgroup, which will fail without
	// proper mocking
	err := cgroup.FreezeSnapProcesses("foo")
	c.Check(err, ErrorMatches, fmt.Sprintf("open %s/proc/%v/cgroup: no such file or directory", dirs.GlobalRootDir, pid))

	procPidCgroup := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%v/cgroup", pid))
	c.Assert(os.MkdirAll(filepath.Dir(procPidCgroup), 0755), IsNil)
	// mock our own group
	c.Assert(ioutil.WriteFile(procPidCgroup, []byte("0::/system.slice/snap.foo.app.own-own-own.scope"), 0755), IsNil)
	// prepare the stage
	for _, p := range []string{gOwn, g1, g2, g3, g4, canary} {
		c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
		c.Assert(ioutil.WriteFile(p, []byte("0"), 0644), IsNil)
	}

	c.Assert(cgroup.FreezeSnapProcesses("foo"), IsNil)
	// our own group is not frozen
	c.Assert(gOwn, testutil.FileEquals, "0")
	// canaries have not been changed
	c.Assert(canary, testutil.FileEquals, "0")
	// other snap groups are frozen
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "1")
	}
}

func (s *freezerV2Suite) TestThawSnapProcessesV2(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// app started by root
	g1 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.app.1234-1234-1234.scope/cgroup.freeze")
	// service started by systemd
	g2 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.svc.service/cgroup.freeze")
	// user applications
	g3 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1234.slice/user@1234.service/snap.foo.user-app.1234-1234-1234.scope/cgroup.freeze")
	// user service
	g4 := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1234.slice/user@1234.service/snap.foo.user-svc.service/cgroup.freeze")
	canary := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.canary.svc.service/cgroup.freeze")
	// a subgroup of the group of a snap
	canarySubgroup := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.svc.service/snap.foo.subgroup.scope/cgroup.freeze")

	// thawing when no groups exist does not break anything
	c.Assert(cgroup.ThawSnapProcesses("foo"), IsNil)

	for _, p := range []string{g1, g2, g3, g4, canary} {
		_, err := os.Stat(p)
		c.Assert(os.IsNotExist(err), Equals, true)
	}

	// prepare the stage
	for _, p := range []string{g1, g2, g3, g4, canary, canarySubgroup} {
		c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
		// groups aren't frozen
		c.Assert(ioutil.WriteFile(p, []byte("0"), 0644), IsNil)
	}

	c.Assert(cgroup.ThawSnapProcesses("foo"), IsNil)
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "0")
	}
	// canaries are still unfrozen
	c.Assert(canary, testutil.FileEquals, "0")
	c.Assert(canarySubgroup, testutil.FileEquals, "0")

	for _, p := range []string{g1, g2, g3, g4, canary, canarySubgroup} {
		// make them appear frozen
		c.Assert(ioutil.WriteFile(p, []byte("1"), 0644), IsNil)
	}
	c.Assert(cgroup.ThawSnapProcesses("foo"), IsNil)
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "0")
	}
	c.Assert(canary, testutil.FileEquals, "1")
	c.Assert(canarySubgroup, testutil.FileEquals, "1")

	// freeze only some the groups groups
	for _, p := range []string{g2, g3} {
		c.Assert(ioutil.WriteFile(p, []byte("1"), 0644), IsNil)
	}
	c.Assert(cgroup.ThawSnapProcesses("foo"), IsNil)
	// all are frozen again
	for _, p := range []string{g1, g2, g3, g4} {
		c.Check(p, testutil.FileEquals, "0")
	}
}

func (s *freezerV2Suite) TestFreezeThawSnapProcessesV2ErrWalking(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// app started by root
	g := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.app.1234-1234-1234.scope/cgroup.freeze")
	gUnfreeze := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice/snap.foo.svc.service/cgroup.freeze")

	pid := os.Getpid()
	procPidCgroup := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%v/cgroup", pid))
	c.Assert(os.MkdirAll(filepath.Dir(procPidCgroup), 0755), IsNil)
	// mock our own group
	c.Assert(ioutil.WriteFile(procPidCgroup, []byte("0::/system.slice/snap.foo.app.own-own-own.scope"), 0755), IsNil)
	// prepare the stage
	c.Assert(os.MkdirAll(filepath.Dir(g), 0755), IsNil)
	c.Assert(ioutil.WriteFile(g, []byte("0"), 0644), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(gUnfreeze), 0755), IsNil)
	c.Assert(ioutil.WriteFile(gUnfreeze, []byte("1"), 0644), IsNil)

	c.Assert(os.Chmod(filepath.Dir(g), 0000), IsNil)
	// make the cleanup happy
	defer os.Chmod(filepath.Dir(g), 0755)

	// freeze tries thawing on errors, so we'll observe both errors
	err := cgroup.FreezeSnapProcesses("foo")
	// go 1.10+ slightly changed the order of calls in filepath.Walk(), make
	// sure the error check matches both
	c.Check(err, ErrorMatches, `cannot finish freezing processes of snap "foo":( cannot freeze processes of snap "foo",)? open .*/sys/fs/cgroup/system.slice/snap.foo.app.1234.1234.1234.scope(/cgroup.freeze)?: permission denied`)
	// other group was unfrozen
	c.Check(gUnfreeze, testutil.FileEquals, "0")

	c.Assert(ioutil.WriteFile(gUnfreeze, []byte("1"), 0644), IsNil)
	// make file access fail
	c.Assert(os.Chmod(filepath.Dir(g), 0755), IsNil)
	c.Assert(os.Chmod(g, 0000), IsNil)
	// other group was unfrozen
	err = cgroup.FreezeSnapProcesses("foo")
	c.Check(err, ErrorMatches, `cannot finish freezing processes of snap "foo": cannot freeze processes of snap "foo", open .*/sys/fs/cgroup/system.slice/snap.foo.app.1234.1234.1234.scope/cgroup.freeze: permission denied`)
	// other group was unfrozen
	c.Check(gUnfreeze, testutil.FileEquals, "0")

	// thawing fails likewise
	err = cgroup.ThawSnapProcesses("foo")
	c.Check(err, ErrorMatches, `cannot thaw processes of snap "foo", open .*/sys/fs/cgroup/system.slice/snap.foo.app.1234.1234.1234.scope/cgroup.freeze: permission denied`)
	// other group was unfrozen
	c.Check(gUnfreeze, testutil.FileEquals, "0")

	// make unfreezing fail
	c.Assert(ioutil.WriteFile(gUnfreeze, []byte("1"), 0644), IsNil)
	c.Assert(os.Chmod(filepath.Dir(gUnfreeze), 0000), IsNil)
	defer os.Chmod(filepath.Dir(gUnfreeze), 0755)

	err = cgroup.FreezeSnapProcesses("foo")
	// but the unfreeze errors are ignored anyuway
	c.Check(err, ErrorMatches, `cannot finish freezing processes of snap "foo": cannot freeze processes of snap "foo", open .*/sys/fs/cgroup/system.slice/snap.foo.app.1234.1234.1234.scope/cgroup.freeze: permission denied`)
	// the other group is unmodified
	os.Chmod(filepath.Dir(gUnfreeze), 0755)
	c.Check(gUnfreeze, testutil.FileEquals, "1")
}
