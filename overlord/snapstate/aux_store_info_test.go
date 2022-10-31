// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2022 Canonical Ltd
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

package snapstate_test

import (
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

type auxInfoSuite struct{}

var _ = check.Suite(&auxInfoSuite{})

func (s *auxInfoSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *auxInfoSuite) TestAuxStoreInfoFilename(c *check.C) {
	// precondition check
	filename := snapstate.AuxStoreInfoFilename("some-snap-id")
	c.Check(filename, check.Equals, filepath.Join(dirs.SnapAuxStoreInfoDir, "some-snap-id.json"))
}

func (s *auxInfoSuite) TestAuxStoreInfoRoundTrip(c *check.C) {
	media := snap.MediaInfos{{Type: "1-2-3-testing"}}
	info := &snap.Info{SuggestedName: "some-snap"}
	info.SnapID = "some-id"
	filename := snapstate.AuxStoreInfoFilename(info.SnapID)
	c.Assert(osutil.FileExists(filename), check.Equals, false)
	c.Check(snapstate.RetrieveAuxStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 0)
	c.Check(info.Website(), check.Equals, "")
	c.Check(info.StoreURL, check.Equals, "")

	aux := &snapstate.AuxStoreInfo{
		Media:    media,
		Website:  "http://example.com/some-snap",
		StoreURL: "https://snapcraft.io/some-snap",
	}
	c.Assert(snapstate.KeepAuxStoreInfo(info.SnapID, aux), check.IsNil)
	c.Check(osutil.FileExists(filename), check.Equals, true)

	c.Assert(snapstate.RetrieveAuxStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 1)
	c.Check(info.Media, check.DeepEquals, media)
	c.Check(info.Website(), check.Equals, "http://example.com/some-snap")
	c.Check(info.StoreURL, check.Equals, "https://snapcraft.io/some-snap")
	info.Media = nil
	info.LegacyWebsite = ""
	info.StoreURL = ""

	info.EditedLinks = map[string][]string{
		"website": {"http://newer-website-com"},
	}
	c.Assert(snapstate.RetrieveAuxStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 1)
	c.Check(info.Media, check.DeepEquals, media)
	c.Check(info.Website(), check.Equals, "http://newer-website-com")
	c.Check(info.LegacyWebsite, check.Equals, "")
	c.Check(info.StoreURL, check.Equals, "https://snapcraft.io/some-snap")
	info.Media = nil
	info.EditedLinks = nil
	info.LegacyWebsite = ""
	info.StoreURL = ""

	c.Assert(snapstate.DiscardAuxStoreInfo(info.SnapID), check.IsNil)
	c.Assert(osutil.FileExists(filename), check.Equals, false)

	c.Check(snapstate.RetrieveAuxStoreInfo(info), check.IsNil)
	c.Check(info.Media, check.HasLen, 0)
	c.Check(info.Website(), check.Equals, "")
	c.Check(info.StoreURL, check.Equals, "")

	c.Check(snapstate.DiscardAuxStoreInfo(info.SnapID), check.IsNil)
}
