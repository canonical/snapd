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

package prompting_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

type tagsSuite struct{}

var _ = Suite(&tagsSuite{})

func (*tagsSuite) TestInterfaceFromTagsets(c *C) {
	restore := prompting.MockApparmorInterfaceForMetadataTag(func(tag string) (string, bool) {
		switch tag {
		case "tag1":
			return "iface1", true
		case "tag2":
			return "iface2", true
		case "tag3":
			return "iface3", true
		default:
			return "", false
		}
	})
	defer restore()

	for _, testCase := range []struct {
		tagsets notify.TagsetMap
		iface   string
		errStr  string
	}{
		{
			tagsets: notify.TagsetMap{},
			errStr:  prompting_errors.ErrNoInterfaceTags.Error(),
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "bar"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"baz"},
			},
			errStr: prompting_errors.ErrNoInterfaceTags.Error(),
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "bar"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag1"},
			},
			errStr: "cannot find interface which applies to permission: read",
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "tag1"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag2", "bar"},
			},
			errStr: "cannot find interface which applies to all permissions",
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "tag1", "tag2"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag2", "bar", "tag3"},
			},
			iface: "iface2",
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "tag1", "tag2", "tag3"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag1", "bar", "tag2"},
			},
			iface: "iface2",
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"tag1", "tag3", "tag2", "foo"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag1", "tag3", "foo"},
			},
			iface: "iface3",
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"tag1", "tag3", "tag2"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag1", "tag2", "tag3"},
			},
			iface: "iface2", // it's a tie, but "iface2" < "iface3"
		},
	} {
		result, err := prompting.InterfaceFromTagsets(testCase.tagsets)

		if testCase.errStr == "" {
			c.Check(err, IsNil)
			c.Check(result, Equals, testCase.iface)
		} else {
			c.Check(err, ErrorMatches, testCase.errStr)
			c.Check(result, Equals, "")
		}
	}
}
