// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package notify_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type tagsSuite struct{}

var _ = Suite(&tagsSuite{})

func (*tagsSuite) TestInterfaceForPermission(c *C) {
	restore := notify.MockApparmorInterfaceForMetadataTag(func(tag string) (string, bool) {
		switch tag {
		case "foo":
			return "iface1", true
		case "qux":
			return "iface2", true
		default:
			return "", false
		}
	})
	defer restore()

	for _, testCase := range []struct {
		tagsetMap notify.TagsetMap
		perm      notify.AppArmorPermission
		tags      notify.MetadataTags
		iface     string
	}{
		{
			tagsetMap: nil,
			perm:      notify.FilePermission(0b1010),
			tags:      notify.MetadataTags{},
			iface:     "",
		},
		{
			tagsetMap: notify.TagsetMap{notify.FilePermission(0b0011): notify.MetadataTags{"foo", "bar"}},
			perm:      notify.FilePermission(0b1100),
			tags:      notify.MetadataTags{},
			iface:     "",
		},
		{
			tagsetMap: notify.TagsetMap{notify.FilePermission(0b0011): notify.MetadataTags{"foo", "bar"}},
			perm:      notify.FilePermission(0b1010),
			tags:      notify.MetadataTags{"foo", "bar"},
			iface:     "iface1",
		},
		{
			tagsetMap: notify.TagsetMap{
				notify.FilePermission(0b0101): notify.MetadataTags{"foo", "bar"},
				notify.FilePermission(0b0010): notify.MetadataTags{"baz", "qux"},
				notify.FilePermission(0b1000): notify.MetadataTags{"fizz", "buzz"},
			},
			perm:  notify.FilePermission(0b0110),
			tags:  notify.MetadataTags{"baz", "qux", "foo", "bar"},
			iface: "iface2",
		},
		{
			tagsetMap: notify.TagsetMap{
				notify.FilePermission(0b0101): notify.MetadataTags{"foo", "bar"},
				notify.FilePermission(0b0010): notify.MetadataTags{"baz", "qux"},
				notify.FilePermission(0b1000): notify.MetadataTags{"fizz", "buzz"},
			},
			perm:  notify.FilePermission(0b0011),
			tags:  notify.MetadataTags{"foo", "bar", "baz", "qux"},
			iface: "iface1",
		},
		{
			tagsetMap: notify.TagsetMap{
				notify.FilePermission(0b0101): notify.MetadataTags{"foo", "bar"},
				notify.FilePermission(0b0010): notify.MetadataTags{"baz", "qux"},
				notify.FilePermission(0b1000): notify.MetadataTags{"fizz", "buzz"},
			},
			perm:  notify.FilePermission(0b1110),
			tags:  notify.MetadataTags{"baz", "qux", "foo", "bar", "fizz", "buzz"},
			iface: "iface2",
		},
		{
			tagsetMap: notify.TagsetMap{
				notify.FilePermission(0b0101): notify.MetadataTags{"foo", "bar"},
				notify.FilePermission(0b0010): notify.MetadataTags{"baz", "qux"},
				notify.FilePermission(0b1000): notify.MetadataTags{"fizz", "buzz"},
			},
			perm:  notify.FilePermission(0b1011),
			tags:  notify.MetadataTags{"foo", "bar", "baz", "qux", "fizz", "buzz"},
			iface: "iface1",
		},
	} {
		result := notify.MetadataTagsForPermission(testCase.tagsetMap, testCase.perm)
		c.Check(result, DeepEquals, testCase.tags, Commentf("testCase: %+v", testCase))

		iface, ok := testCase.tagsetMap.InterfaceForPermission(testCase.perm)
		c.Check(iface, Equals, testCase.iface, Commentf("testCase: %+v", testCase))
		if testCase.iface == "" {
			c.Check(ok, Equals, false)
		} else {
			c.Check(ok, Equals, true)
		}
	}
}
