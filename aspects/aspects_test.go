// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package aspects_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/testutil"
)

type aspectSuite struct{}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&aspectSuite{})

func (*aspectSuite) TestAspectDirectory(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("system/network", map[string]interface{}{
		"wifi-setup": []map[string]string{
			{"name": "ssids", "path": "wifi.ssids"},
			{"name": "ssid", "path": "wifi.ssid"},
			{"name": "top-level", "path": "top-level"},
			{"name": "wifi", "path": "wifi"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	wsAspect := aspectDir.Aspect("wifi-setup")
	err = wsAspect.Set("ssid", "my-ssid")
	c.Assert(err, IsNil)

	err = wsAspect.Set("ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	var ssid string
	err = wsAspect.Get("ssid", &ssid)
	c.Assert(err, IsNil)
	c.Check(ssid, Equals, "my-ssid")

	var ssids []string
	err = wsAspect.Get("ssids", &ssids)
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, []string{"one", "two"})

	var topLevel string
	err = wsAspect.Get("top-level", &topLevel)
	c.Assert(err, testutil.ErrorIs, &aspects.ErrNotFound{})

	err = wsAspect.Set("top-level", "randomValue")
	c.Assert(err, IsNil)

	err = wsAspect.Get("top-level", &topLevel)
	c.Assert(err, IsNil)
	c.Check(topLevel, Equals, "randomValue")

	err = wsAspect.Get("wifi", &topLevel)
	c.Assert(err, ErrorMatches, `cannot read "wifi" into variable of type "\*string" because it maps to JSON object`)
}

func (s *aspectSuite) TestAspectsWithAccess(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("dir", map[string]interface{}{
		"foo": []map[string]string{
			{"name": "default", "path": "path.default"},
			{"name": "read-write", "path": "path.read-write", "access": "read-write"},
			{"name": "read-only", "path": "path.read-only", "access": "read"},
			{"name": "write-only", "path": "path.write-only", "access": "write"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectDir.Aspect("foo")

	for _, t := range []struct {
		name   string
		getErr string
		setErr string
	}{
		{
			name: "read-write",
		},
		{
			// defaults to "read-write"
			name: "default",
		},
		{
			name: "read-only",
			// unrelated error
			getErr: `sub-key "read-only" not found`,
			setErr: `cannot set "read-only": path is not writeable`,
		},
		{
			name:   "write-only",
			getErr: `cannot get "write-only": path is not readable`,
		},
	} {
		cmt := Commentf("sub-test %q failed", t.name)
		err := aspect.Set(t.name, "thing")
		if t.setErr != "" {
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		var value string
		err = aspect.Get(t.name, &value)
		if t.getErr != "" {
			c.Assert(err.Error(), Equals, t.getErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}
	}
}
