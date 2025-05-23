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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type metadataSuite struct{}

var _ = Suite(&metadataSuite{})

func (s *metadataSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *metadataSuite) TestInstallStoreMetadataUndo(c *C) {
	const snapID = "my-snap-id"
	for _, testCase := range []struct {
		hasOtherInstances    bool
		firstInstall         bool
		auxShouldExistAfter  bool
		iconShouldExistAfter bool
	}{
		// undo should remove:
		// - auxinfo iff there are no other instances and it's an install
		// - icon iff there are no other instances
		{
			hasOtherInstances:    false,
			firstInstall:         true,
			auxShouldExistAfter:  false,
			iconShouldExistAfter: false,
		},
		{
			hasOtherInstances:    true,
			firstInstall:         true,
			auxShouldExistAfter:  true,
			iconShouldExistAfter: true,
		},
		{
			hasOtherInstances:    false,
			firstInstall:         false,
			auxShouldExistAfter:  true,
			iconShouldExistAfter: false,
		},
		{
			hasOtherInstances:    true,
			firstInstall:         false,
			auxShouldExistAfter:  true,
			iconShouldExistAfter: true,
		},
	} {
		// Need a new tmp root dir so test cases don't collide
		dirs.SetRootDir(c.MkDir())

		// set up icon in the download pool
		iconContents := []byte("icon contents")
		c.Assert(os.MkdirAll(filepath.Dir(backend.IconDownloadFilename(snapID)), 0o755), IsNil)
		c.Assert(os.WriteFile(backend.IconDownloadFilename(snapID), iconContents, 0o644), IsNil)

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

		if testCase.auxShouldExistAfter {
			checkWrittenInfo()
		} else {
			c.Check(backend.AuxStoreInfoFilename(snapID), testutil.FileAbsent)
		}

		if testCase.iconShouldExistAfter {
			c.Check(backend.IconInstallFilename(snapID), testutil.FileEquals, iconContents)
		} else {
			c.Check(backend.IconInstallFilename(snapID), testutil.FileAbsent)
		}
	}
}

func (s *metadataSuite) TestStoreMetadataEmptySnapID(c *C) {
	const snapID = ""
	var aux backend.AuxStoreInfo
	var linkCtx backend.LinkContext // empty, doesn't matter for this test
	const hasOtherInstances = false
	// check that empty snapID does not return an error
	undo, err := backend.InstallStoreMetadata(snapID, aux, linkCtx)
	c.Check(err, IsNil)
	c.Check(undo, NotNil)
	c.Check(backend.UninstallStoreMetadata(snapID, linkCtx), IsNil)
	c.Check(backend.DiscardStoreMetadata(snapID, hasOtherInstances), IsNil)
}

func (s *metadataSuite) TestInstallStoreMetadataNoIcon(c *C) {
	const snapID = "my-id"
	var aux backend.AuxStoreInfo    // contents don't matter for this test
	var linkCtx backend.LinkContext // empty, doesn't matter for this test

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	// Icon is not present in the icons download pool

	// Check that lack of icon does not cause an error
	_, err := backend.InstallStoreMetadata(snapID, aux, linkCtx)
	c.Check(err, IsNil)

	// but a debug log is recorded
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("cannot link snap icon for snap my-id: icon for snap: %v", fs.ErrNotExist))
}

func (s *metadataSuite) TestDiscardStoreMetadata(c *C) {
	for _, testCase := range []struct {
		inPool         bool
		installed      bool
		auxInfo        bool
		otherInstances bool
		expectRemoved  bool
	}{
		{
			inPool:         true,
			installed:      true,
			auxInfo:        true,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         true,
			installed:      false,
			auxInfo:        true,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         false,
			installed:      true,
			auxInfo:        true,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         false,
			installed:      false,
			auxInfo:        true,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         true,
			installed:      true,
			auxInfo:        false,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         true,
			installed:      false,
			auxInfo:        false,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         false,
			installed:      true,
			auxInfo:        false,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         false,
			installed:      false,
			auxInfo:        false,
			otherInstances: false,
			expectRemoved:  true,
		},
		{
			inPool:         true,
			installed:      true,
			auxInfo:        true,
			otherInstances: true,
			expectRemoved:  false,
		},
	} {
		// Need a new tmp root dir so test cases don't collide
		dirs.SetRootDir(c.MkDir())

		const snapID = "my-id"
		var iconContents = []byte("icon contents")
		var auxinfo = []byte("some links")

		if testCase.inPool {
			c.Assert(os.MkdirAll(filepath.Dir(backend.IconDownloadFilename(snapID)), 0o755), IsNil)
			c.Assert(os.WriteFile(backend.IconDownloadFilename(snapID), iconContents, 0o644), IsNil)
		}

		if testCase.installed {
			c.Assert(os.MkdirAll(filepath.Dir(backend.IconInstallFilename(snapID)), 0o755), IsNil)
			c.Assert(os.WriteFile(backend.IconInstallFilename(snapID), iconContents, 0o644), IsNil)
		}

		if testCase.auxInfo {
			c.Assert(os.MkdirAll(filepath.Dir(backend.AuxStoreInfoFilename(snapID)), 0o755), IsNil)
			c.Assert(os.WriteFile(backend.AuxStoreInfoFilename(snapID), auxinfo, 0o644), IsNil)
		}

		err := backend.DiscardStoreMetadata(snapID, testCase.otherInstances)
		c.Check(err, IsNil)

		if testCase.expectRemoved {
			c.Check(backend.IconDownloadFilename(snapID), testutil.FileAbsent)
			c.Check(backend.IconInstallFilename(snapID), testutil.FileAbsent)
			c.Check(backend.AuxStoreInfoFilename(snapID), testutil.FileAbsent)
		} else {
			if testCase.inPool {
				c.Check(backend.IconDownloadFilename(snapID), testutil.FileEquals, iconContents)
			}
			if testCase.installed {
				c.Check(backend.IconInstallFilename(snapID), testutil.FileEquals, iconContents)
			}
			if testCase.auxInfo {
				c.Check(backend.AuxStoreInfoFilename(snapID), testutil.FileEquals, auxinfo)
			}
		}
	}
}
