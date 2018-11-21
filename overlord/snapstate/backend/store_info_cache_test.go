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

package backend_test

import (
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/snap"
)

type storeCacheSuite struct {
	be backend.Backend
}

var _ = check.Suite(&storeCacheSuite{})

func (s *storeCacheSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *storeCacheSuite) TestStoreCacheRoundTrip(c *check.C) {
	media := snap.MediaInfos{{Type: "1-2-3-testing"}}
	info := &snap.Info{SuggestedName: "some-snap"}
	info.SnapID = "some-id"
	filename := backend.SnapStoreInfoCacheFilename("some-snap")
	c.Assert(osutil.FileExists(filename), check.Equals, false)
	c.Check(backend.AttachStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 0)

	c.Assert(s.be.CacheStoreInfo(info.InstanceName(), &backend.StoreInfo{
		Media: media,
	}), check.IsNil)
	c.Check(osutil.FileExists(filename), check.Equals, true)

	c.Assert(backend.AttachStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 1)
	c.Check(info.Media, check.DeepEquals, media)
	info.Media = nil

	c.Assert(s.be.DeleteStoreInfoCache(info.InstanceName()), check.IsNil)
	c.Assert(osutil.FileExists(filename), check.Equals, false)

	c.Check(backend.AttachStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 0)

	c.Check(s.be.DeleteStoreInfoCache(info.SnapID), check.IsNil)
}
