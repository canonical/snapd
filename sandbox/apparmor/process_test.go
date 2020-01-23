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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

func (s *apparmorSuite) TestLabelFromPid(c *C) {
	d := c.MkDir()
	restore := apparmor.MockFsRootPath(d)
	defer restore()

	// When no /proc/$pid/attr/current exists, assume unconfined
	label, err := apparmor.LabelFromPid(42)
	c.Check(label, Equals, "unconfined")
	c.Check(err, IsNil)

	procFile := filepath.Join(d, "proc/42/attr/current")
	c.Assert(os.MkdirAll(filepath.Dir(procFile), 0755), IsNil)
	for _, t := range []struct {
		contents []byte
		label    string
	}{
		{[]byte("unconfined\n"), "unconfined"},
		{[]byte("/usr/sbin/cupsd (enforce)\n"), "/usr/sbin/cupsd"},
		{[]byte("snap.foo.app (complain)\n"), "snap.foo.app"},
	} {
		c.Assert(ioutil.WriteFile(procFile, t.contents, 0644), IsNil)
		label, err := apparmor.LabelFromPid(42)
		c.Check(err, IsNil)
		c.Check(label, Equals, t.label)
	}
}

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
