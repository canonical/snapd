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
		err     error
	}{
		{
			tagsets: notify.TagsetMap{},
			err:     prompting_errors.ErrNoInterfaceTags,
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "bar"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"baz"},
			},
			err: prompting_errors.ErrNoInterfaceTags,
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "bar"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag1"},
			},
			err: prompting_errors.ErrNoCommonInterface,
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "tag1"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag2", "bar"},
			},
			err: prompting_errors.ErrMultipleInterfaces,
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "tag1", "tag2"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag2", "bar", "tag3"},
			},
			err: prompting_errors.ErrMultipleInterfaces,
		},
		{
			tagsets: notify.TagsetMap{
				notify.AA_MAY_READ:  notify.MetadataTags{"foo", "tag2"},
				notify.AA_MAY_WRITE: notify.MetadataTags{"tag2", "bar"},
			},
			iface: "iface2",
		},
	} {
		iface, err := prompting.InterfaceFromTagsets(testCase.tagsets)

		c.Check(err, Equals, testCase.err)
		c.Check(iface, Equals, testCase.iface)
	}
}
