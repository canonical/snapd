// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type scanningSuite struct {
	testutil.BaseTest
	rootDir string
}

var _ = Suite(&scanningSuite{})

func (s *scanningSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func mustParseTag(tag string) naming.SecurityTag {
	parsedTag, err := naming.ParseSecurityTag(tag)
	if err != nil {
		panic(err)
	}
	return parsedTag
}

func (s *scanningSuite) TestSecurityTagFromCgroupPath(c *C) {
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.foo.service"), DeepEquals, mustParseTag("snap.foo.foo"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.bar.service"), DeepEquals, mustParseTag("snap.foo.bar"))
	// We should be able to match both the old and new naming convention for scope units.
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.bar-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.bar"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.bar-baz-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.bar-baz"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.hook.bar-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.hook.bar"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.hook.bar-baz-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.hook.bar-baz"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.bar.54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.bar"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.bar-baz.54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.bar-baz"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.hook.bar.54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.hook.bar"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.hook.bar-baz.54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo.hook.bar-baz"))
	// We are not confused by snapd things.
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.service"), IsNil)
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snapd.service"), IsNil)
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.mount"), IsNil)
	// Real data looks like this.
	c.Check(cgroup.SecurityTagFromCgroupPath("snap.test-snapd-refresh.sh-d854bd35-2457-4ac8-b494-06061d74df33.scope"), DeepEquals, mustParseTag("snap.test-snapd-refresh.sh"))
	c.Check(cgroup.SecurityTagFromCgroupPath("snap.test-snapd-refresh.hook.configure-d854bd35-2457-4ac8-b494-06061d74df33.scope"), DeepEquals, mustParseTag("snap.test-snapd-refresh.hook.configure"))
	// Trailing slashes are automatically handled.
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.foo.service/"), DeepEquals, mustParseTag("snap.foo.foo"))
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo\\x2bcomp.hook.install.54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope"), DeepEquals, mustParseTag("snap.foo+comp.hook.install"))
}

// Returns the number of occurrences of 'needle' in the array 'arr'
func matchesInArray(arr []string, needle string) int {
	counter := 0
	for _, v := range arr {
		if v == needle {
			counter++
		}
	}
	return counter
}

func (s *scanningSuite) writePids(c *C, dir string, pids []int) string {
	var buf bytes.Buffer
	for _, pid := range pids {
		fmt.Fprintf(&buf, "%d\n", pid)
	}

	var path string
	ver, err := cgroup.Version()
	c.Assert(err, IsNil)
	c.Assert(ver == cgroup.V1 || ver == cgroup.V2, Equals, true)
	switch ver {
	case cgroup.V1:
		path = filepath.Join(s.rootDir, "/sys/fs/cgroup/systemd", dir)
	case cgroup.V2:
		path = filepath.Join(s.rootDir, "/sys/fs/cgroup", dir)
	}

	c.Assert(os.MkdirAll(path, 0755), IsNil)
	finalPath := filepath.Join(path, "cgroup.procs")
	c.Assert(os.WriteFile(finalPath, buf.Bytes(), 0644), IsNil)
	return finalPath
}

func (s *scanningSuite) TestPidsOfSnapEmpty(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	// Not having any cgroup directories is not an error.
	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, HasLen, 0)
}

func (s *scanningSuite) TestPathsOfSnapEmpty(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	// Not having any cgroup directories is not an error.
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	paths, err := cgroup.InstancePathsOfSnap("pkg", options)
	c.Assert(err, IsNil)
	c.Check(paths, HasLen, 0)
}

func (s *scanningSuite) TestPidsOfSnapUnrelatedStuff(c *C) {
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// Things that are not related to the snap are not being picked up.
		s.writePids(c, "udisks2.service", []int{100})
		s.writePids(c, "snap..service", []int{101})
		s.writePids(c, "snap..scope", []int{102})
		s.writePids(c, "snap.*.service", []int{103})
		s.writePids(c, "snap.*.scope", []int{104})
		s.writePids(c, "snapd.service", []int{105})
		s.writePids(c, "snap-spotify-35.mount", []int{106})

		pids, err := cgroup.PidsOfSnap("pkg")
		c.Assert(err, IsNil, comment)
		c.Check(pids, HasLen, 0, comment)
	}
}

func (s *scanningSuite) TestPathsOfSnapUnrelatedStuff(c *C) {
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// Things that are not related to the snap are not being picked up.
		s.writePids(c, "udisks2.service", []int{100})
		s.writePids(c, "snap..service", []int{101})
		s.writePids(c, "snap..scope", []int{102})
		s.writePids(c, "snap.*.service", []int{103})
		s.writePids(c, "snap.*.scope", []int{104})
		s.writePids(c, "snapd.service", []int{105})
		s.writePids(c, "snap-spotify-35.mount", []int{106})

		paths, err := cgroup.InstancePathsOfSnap("pkg", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 0, comment)
	}
}

func (s *scanningSuite) TestPidsOfSnapSecurityTags(c *C) {
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// Pids are collected and assigned to bins by security tag
		s.writePids(c, "system.slice/snap.pkg.hook.configure-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{1})
		s.writePids(c, "system.slice/snap.pkg.daemon.service", []int{2})

		pids, err := cgroup.PidsOfSnap("pkg")
		c.Assert(err, IsNil, comment)
		c.Check(pids, DeepEquals, map[string][]int{
			"snap.pkg.hook.configure": {1},
			"snap.pkg.daemon":         {2},
		}, comment)
	}
}

func (s *scanningSuite) TestPathsOfSnapWithSecurityTags(c *C) {
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// Pids are collected and assigned to bins by security tag
		path1 := s.writePids(c, "system.slice/snap.pkg.hook.configure-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{1})
		path2 := s.writePids(c, "system.slice/snap.pkg.daemon.service", []int{2})

		paths, err := cgroup.InstancePathsOfSnap("pkg", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 2)
		c.Check(matchesInArray(paths, filepath.Dir(path1)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path2)), Equals, 1)
	}
}

func (s *scanningSuite) TestPidsOfInstances(c *C) {
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// Instances are not confused between themselves and between the non-instance version.
		s.writePids(c, "system.slice/snap.pkg_prod.daemon.service", []int{1})
		s.writePids(c, "system.slice/snap.pkg_devel.daemon.service", []int{2})
		s.writePids(c, "system.slice/snap.pkg.daemon.service", []int{3})

		// The main one
		pids, err := cgroup.PidsOfSnap("pkg")
		c.Assert(err, IsNil, comment)
		c.Check(pids, DeepEquals, map[string][]int{
			"snap.pkg.daemon": {3},
		}, comment)

		// The development one
		pids, err = cgroup.PidsOfSnap("pkg_devel")
		c.Assert(err, IsNil, comment)
		c.Check(pids, DeepEquals, map[string][]int{
			"snap.pkg_devel.daemon": {2},
		}, comment)

		// The production one
		pids, err = cgroup.PidsOfSnap("pkg_prod")
		c.Assert(err, IsNil, comment)
		c.Check(pids, DeepEquals, map[string][]int{
			"snap.pkg_prod.daemon": {1},
		}, comment)
	}
}

func (s *scanningSuite) TestPathsOfInstances(c *C) {
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// Instances are not confused between themselves and between the non-instance version.
		path1 := s.writePids(c, "system.slice/snap.pkg_prod.daemon.service", []int{1})
		path2 := s.writePids(c, "system.slice/snap.pkg_devel.daemon.service", []int{2})
		path3 := s.writePids(c, "system.slice/snap.pkg.daemon.service", []int{3})

		// The main one
		paths, err := cgroup.InstancePathsOfSnap("pkg", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 1)
		c.Check(paths[0], Equals, filepath.Dir(path3))

		// The development one
		paths, err = cgroup.InstancePathsOfSnap("pkg_devel", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 1)
		c.Check(paths[0], Equals, filepath.Dir(path2))

		// The production one
		paths, err = cgroup.InstancePathsOfSnap("pkg_prod", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 1)
		c.Check(paths[0], Equals, filepath.Dir(path1))
	}
}

func (s *scanningSuite) TestPidsOfAggregation(c *C) {
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// A single snap may be invoked by multiple users in different sessions.
		// All of their PIDs are collected though.
		s.writePids(c, "user.slice/user-1000.slice/user@1000.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e1.scope", []int{1}) // mock 1st invocation
		s.writePids(c, "user.slice/user-1000.slice/user@1000.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e2.scope", []int{2}) // mock fork() by pid 1
		s.writePids(c, "user.slice/user-1001.slice/user@1001.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e3.scope", []int{3}) // mock 2nd invocation
		s.writePids(c, "user.slice/user-1001.slice/user@1001.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e4.scope", []int{4}) // mock fork() by pid 3

		pids, err := cgroup.PidsOfSnap("pkg")
		c.Assert(err, IsNil, comment)
		c.Check(pids, DeepEquals, map[string][]int{
			"snap.pkg.app": {1, 2, 3, 4},
		}, comment)
	}
}

func (s *scanningSuite) TestPathsOfAggregation(c *C) {
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// A single snap may be invoked by multiple users in different sessions.
		// All of their PIDs are collected though.
		path1 := s.writePids(c, "user.slice/user-1000.slice/user@1000.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e1.scope", []int{1}) // mock 1st invocation
		path2 := s.writePids(c, "user.slice/user-1000.slice/user@1000.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e2.scope", []int{2}) // mock fork() by pid 1
		path3 := s.writePids(c, "user.slice/user-1001.slice/user@1001.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e3.scope", []int{3}) // mock 2nd invocation
		path4 := s.writePids(c, "user.slice/user-1001.slice/user@1001.service/gnome-shell-wayland.service/snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e4.scope", []int{4}) // mock fork() by pid 3

		paths, err := cgroup.InstancePathsOfSnap("pkg", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 4)
		c.Check(matchesInArray(paths, filepath.Dir(path1)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path2)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path3)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path4)), Equals, 1)
	}
}

func (s *scanningSuite) TestPidsOfSnapUnrelated(c *C) {
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// We are not confusing snaps with other snaps, instances of our snap, and
		// with non-snap hierarchies.
		s.writePids(c, "user.slice/.../snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{1})
		s.writePids(c, "user.slice/.../snap.other.snap-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{2})
		s.writePids(c, "user.slice/.../pkg.service", []int{3})
		s.writePids(c, "user.slice/.../snap.pkg_instance.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{4})

		// Write a file which is not cgroup.procs with the number 666 inside.
		// We want to ensure this is not read by accident.
		f := filepath.Join(s.rootDir, "/sys/fs/cgroup/unrelated.txt")
		c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
		c.Assert(os.WriteFile(f, []byte("666"), 0644), IsNil)

		pids, err := cgroup.PidsOfSnap("pkg")
		c.Assert(err, IsNil, comment)
		c.Check(pids, DeepEquals, map[string][]int{
			"snap.pkg.app": {1},
		}, comment)
	}
}

func (s *scanningSuite) TestPathsOfSnapUnrelated(c *C) {
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		// We are not confusing snaps with other snaps, instances of our snap, and
		// with non-snap hierarchies.
		path1 := s.writePids(c, "user.slice/.../snap.pkg.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{1})
		path2 := s.writePids(c, "user.slice/.../snap.other.snap-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{2})
		path3 := s.writePids(c, "user.slice/.../pkg.service", []int{3})
		path4 := s.writePids(c, "user.slice/.../snap.pkg_instance.app-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", []int{4})

		// Write a file which is not cgroup.procs with the number 666 inside.
		// We want to ensure this is not read by accident.
		f := filepath.Join(s.rootDir, "/sys/fs/cgroup/unrelated.txt")
		c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
		c.Assert(os.WriteFile(f, []byte("666"), 0644), IsNil)

		paths, err := cgroup.InstancePathsOfSnap("pkg", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path1)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path2)), Equals, 0)
		c.Check(matchesInArray(paths, filepath.Dir(path3)), Equals, 0)
		c.Check(matchesInArray(paths, filepath.Dir(path4)), Equals, 0)
	}
}

func (s *scanningSuite) TestContainerPidsAreIgnored(c *C) {
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		s.writePids(c, "user.slice/user-1000.slice/user@1000.service/snap.foo.bar.scope", []int{1})
		s.writePids(c, "system.slice/snap.foo.bar.service", []int{2})
		s.writePids(c, "lxc.payload.my-container/system.slice/snap.foo.bar.service", []int{3})
		s.writePids(c, "machine.slice/snap.foo.bar.service", []int{4})
		s.writePids(c, "docker/snap.foo.bar.service", []int{5})

		pids, err := cgroup.PidsOfSnap("foo")
		c.Assert(err, IsNil, comment)
		c.Check(pids, HasLen, 1, comment)
		c.Check(pids["snap.foo.bar"], testutil.DeepUnsortedMatches, []int{1, 2}, comment)
	}
}

func (s *scanningSuite) TestContainerPathsAreIgnored(c *C) {
	options := cgroup.InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	for _, ver := range []int{cgroup.V2, cgroup.V1} {
		comment := Commentf("cgroup version %v", ver)
		restore := cgroup.MockVersion(ver, nil)
		defer restore()

		path1 := s.writePids(c, "user.slice/user-1000.slice/user@1000.service/snap.foo.bar.scope", []int{1})
		path2 := s.writePids(c, "system.slice/snap.foo.bar.service", []int{2})
		path3 := s.writePids(c, "lxc.payload.my-container/system.slice/snap.foo.bar.service", []int{3})
		path4 := s.writePids(c, "machine.slice/snap.foo.bar.service", []int{4})
		path5 := s.writePids(c, "docker/snap.foo.bar.service", []int{5})

		paths, err := cgroup.InstancePathsOfSnap("foo", options)
		c.Assert(err, IsNil, comment)
		c.Check(paths, HasLen, 2, comment)
		c.Check(matchesInArray(paths, filepath.Dir(path1)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path2)), Equals, 1)
		c.Check(matchesInArray(paths, filepath.Dir(path3)), Equals, 0)
		c.Check(matchesInArray(paths, filepath.Dir(path4)), Equals, 0)
		c.Check(matchesInArray(paths, filepath.Dir(path5)), Equals, 0)
	}
}
