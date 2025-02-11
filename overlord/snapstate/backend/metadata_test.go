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

package backend_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type metadataSuite struct{}

var _ = Suite(&metadataSuite{})

func (s *metadataSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *metadataSuite) TestInstallStoreMetadataRevert(c *C) {
	const snapID = "my-snap-id"
	for _, testCase := range []struct {
		hasOtherInstances bool
		firstInstall      bool
		shouldExistAfter  bool
	}{
		// undo should remove the file iff there are no other instances and it's an install
		{hasOtherInstances: false, firstInstall: true, shouldExistAfter: false},
		{hasOtherInstances: true, firstInstall: true, shouldExistAfter: true},
		{hasOtherInstances: false, firstInstall: false, shouldExistAfter: true},
		{hasOtherInstances: true, firstInstall: false, shouldExistAfter: true},
	} {
		// Need a new tmp root dir so test cases don't collide
		dirs.SetRootDir(c.MkDir())

		c.Assert(backend.AuxStoreInfoFilename(snapID), testutil.FileAbsent)
		aux := backend.AuxStoreInfo{
			Media: snap.MediaInfos{
				snap.MediaInfo{
					Type:   "icon",
					URL:    "http://images.com/my-icon",
					Width:  128,
					Height: 128,
				},
				snap.MediaInfo{
					Type: "website",
					URL:  "http://another.com",
				},
			},
			StoreURL: "https://snapcraft.io/example-snap",
			Website:  "http://example.com",
		}
		linkCtx := backend.LinkContext{
			FirstInstall:      testCase.firstInstall,
			HasOtherInstances: testCase.hasOtherInstances,
		}
		undo, err := backend.InstallStoreMetadata(snapID, aux, linkCtx)
		c.Check(err, IsNil)

		c.Check(backend.AuxStoreInfoFilename(snapID), testutil.FilePresent)

		checkWrittenInfo := func() {
			var info snap.Info
			info.SnapID = snapID
			c.Check(backend.RetrieveAuxStoreInfo(&info), IsNil)
			c.Check(info.Media, DeepEquals, aux.Media)
			c.Check(info.LegacyWebsite, Equals, aux.Website)
			c.Check(info.StoreURL, Equals, aux.StoreURL)
		}
		checkWrittenInfo()

		undo()

		if testCase.shouldExistAfter {
			checkWrittenInfo()
		} else {
			c.Check(backend.AuxStoreInfoFilename(snapID), testutil.FileAbsent)
		}
	}
}

func (s *metadataSuite) TestStoreMetadataEmptySnapID(c *C) {
	const snapID = ""
	var aux backend.AuxStoreInfo
	const hasOtherInstances = false
	var linkCtx backend.LinkContext // empty, doesn't matter for this test
	// check that empty snapID does not return an error
	undo, err := backend.InstallStoreMetadata(snapID, aux, linkCtx)
	c.Check(err, IsNil)
	c.Check(undo, NotNil)
	c.Check(backend.DiscardStoreMetadata(snapID, hasOtherInstances), IsNil)
}

func (s *metadataSuite) TestDiscardStoreMetadataHasOtherInstances(c *C) {
	const snapID = "my-snap-id"
	c.Assert(backend.AuxStoreInfoFilename(snapID), testutil.FileAbsent)
	aux := backend.AuxStoreInfo{
		StoreURL: "https://snapcraft.io/example-snap",
	}
	// Value of linkCtx doesn't matter to InstallStoreMetadata outside of the
	// returned undo, which we ignore here
	var linkCtx backend.LinkContext
	_, err := backend.InstallStoreMetadata(snapID, aux, linkCtx)
	c.Check(err, IsNil)
	c.Check(backend.AuxStoreInfoFilename(snapID), testutil.FilePresent)

	// Check that it does not discard if hasOtherInstances is true
	hasOtherInstances := true
	c.Check(backend.DiscardStoreMetadata(snapID, hasOtherInstances), IsNil)
	c.Assert(backend.AuxStoreInfoFilename(snapID), testutil.FilePresent)

	hasOtherInstances = false
	// Check that it is discarded if hasOtherInstances is false
	c.Check(backend.DiscardStoreMetadata(snapID, hasOtherInstances), IsNil)
	c.Assert(backend.AuxStoreInfoFilename(snapID), testutil.FileAbsent)
}
