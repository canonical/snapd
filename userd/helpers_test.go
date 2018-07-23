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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/userd"
)

type helpersSuite struct{}

var _ = Suite(&helpersSuite{})

var mockCgroup = []byte(`
10:devices:/user.slice
9:cpuset:/
8:net_cls,net_prio:/
7:freezer:/snap.hello-world
6:perf_event:/
5:pids:/user.slice/user-1000.slice/user@1000.service
4:cpu,cpuacct:/
3:memory:/
2:blkio:/
1:name=systemd:/user.slice/user-1000.slice/user@1000.service/gnome-terminal-server.service
0::/user.slice/user-1000.slice/user@1000.service/gnome-terminal-server.service
`)

func (s *helpersSuite) TestSnapFromPid(c *C) {
	root := c.MkDir()
	dirs.SetRootDir(root)

	err := os.MkdirAll(filepath.Join(root, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(root, "proc/333/cgroup"), mockCgroup, 0755)
	c.Assert(err, IsNil)

	snap, err := userd.SnapFromPid(333)
	c.Assert(err, IsNil)
	c.Check(snap, Equals, "hello-world")
}
