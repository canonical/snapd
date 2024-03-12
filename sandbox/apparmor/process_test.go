// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package apparmor_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

func (s *apparmorSuite) TestDecodeLabel(c *C) {
	label := snap.AppSecurityTag("snap_name", "my-app")
	snapName, appName, hookName, err := apparmor.DecodeLabel(label)
	c.Assert(err, IsNil)
	c.Check(snapName, Equals, "snap_name")
	c.Check(appName, Equals, "my-app")
	c.Check(hookName, Equals, "")

	label = snap.HookSecurityTag("snap_name", "my-hook")
	snapName, appName, hookName, err = apparmor.DecodeLabel(label)
	c.Assert(err, IsNil)
	c.Check(snapName, Equals, "snap_name")
	c.Check(appName, Equals, "")
	c.Check(hookName, Equals, "my-hook")

	_, _, _, err = apparmor.DecodeLabel("unconfined")
	c.Assert(err, ErrorMatches, `security label "unconfined" does not belong to a snap`)

	_, _, _, err = apparmor.DecodeLabel("/usr/bin/ntpd")
	c.Assert(err, ErrorMatches, `security label "/usr/bin/ntpd" does not belong to a snap`)
}

func (s *apparmorSuite) TestDecodeLabelUnrecognisedSnapLabel(c *C) {
	_, _, _, err := apparmor.DecodeLabel("snap.weird")
	c.Assert(err, ErrorMatches, `unknown snap related security label "snap.weird"`)
}

func (s *apparmorSuite) TestSnapAppFromPidNewKernelPath(c *C) {
	d := c.MkDir()
	restore := apparmor.MockFsRootPath(d)
	defer restore()

	// when the new file exists we use that one
	newProcFile := filepath.Join(d, "proc/42/attr/apparmor/current")
	c.Assert(os.MkdirAll(filepath.Dir(newProcFile), 0755), IsNil)
	c.Assert(os.WriteFile(newProcFile, []byte("snap.foo.app"), 0644), IsNil)

	oldProcFile := filepath.Join(d, "proc/42/attr/current")
	c.Assert(os.MkdirAll(filepath.Dir(oldProcFile), 0755), IsNil)
	c.Assert(os.WriteFile(oldProcFile, []byte("random-other-unread-data"), 0644), IsNil)

	name, app, hook, err := apparmor.SnapAppFromPid(42)
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "foo")
	c.Assert(app, Equals, "app")
	c.Assert(hook, Equals, "")
}

func (s *apparmorSuite) TestSnapAppFromPid(c *C) {
	d := c.MkDir()
	restore := apparmor.MockFsRootPath(d)
	defer restore()

	// When no /proc/$pid/attr/current exists, assume unconfined
	_, _, _, err := apparmor.SnapAppFromPid(42)
	c.Check(err, ErrorMatches, `security label "unconfined" does not belong to a snap`)

	procFile := filepath.Join(d, "proc/42/attr/current")
	c.Assert(os.MkdirAll(filepath.Dir(procFile), 0755), IsNil)

	c.Assert(os.WriteFile(procFile, []byte("not-read"), 0000), IsNil)
	_, _, _, err = apparmor.SnapAppFromPid(42)
	c.Check(err, ErrorMatches, `open .*/proc/42/attr/current: permission denied`)
	c.Assert(os.Remove(procFile), IsNil)

	for _, t := range []struct {
		contents        string
		name, app, hook string
		err             string
	}{{
		contents: "unconfined\n",
		err:      `security label "unconfined" does not belong to a snap`,
	}, {
		contents: "/usr/sbin/cupsd (enforce)\n",
		err:      `security label "/usr/sbin/cupsd" does not belong to a snap`,
	}, {
		contents: "snap.foo.app (complain)\n",
		name:     "foo",
		app:      "app",
	}, {
		contents: "snap.foo.hook.snap-hook (complain)\n",
		name:     "foo",
		hook:     "snap-hook",
	}, {
		contents: "snap.foo.app.garbage\n",
		err:      `unknown snap related security label "snap.foo.app.garbage"`,
	}, {
		contents: "snap.foo.hook.app.garbage\n",
		err:      `unknown snap related security label "snap.foo.hook.app.garbage"`,
	}} {
		c.Assert(os.WriteFile(procFile, []byte(t.contents), 0644), IsNil)
		name, app, hook, err := apparmor.SnapAppFromPid(42)
		if t.err != "" {
			c.Check(err, ErrorMatches, t.err)
		} else {
			c.Check(err, IsNil)
			c.Check(name, Equals, t.name)
			c.Check(app, Equals, t.app)
			c.Check(hook, Equals, t.hook)
		}
	}
}
