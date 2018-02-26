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

package store

import (
	"encoding/json"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type detailsV2Suite struct{}

var _ = Suite(&detailsV2Suite{})

const (
	coreStoreJSON = `{
  "architectures": [
    "amd64"
  ],
  "base": null,
  "confinement": "strict",
  "contact": "mailto:snappy-canonical-storeaccount@canonical.com",
  "created-at": "2018-01-22T07:49:19.440720+00:00",
  "description": "The core runtime environment for snapd",
  "download": {
     "sha3-384": "b691f6dde3d8022e4db563840f0ef82320cb824b6292ffd027dbc838535214dac31c3512c619beaf73f1aeaf35ac62d5",
     "size": 85291008,
     "url": "https://api.snapcraft.io/api/v1/snaps/download/99T7MUlRhtI3U0QFgl5mXXESAiSwt776_3887.snap",
     "deltas": []
  },
  "epoch": {
     "read": [0],
     "write": [0]
  },
  "license": null,
  "name": "core",
  "prices": {},
  "private": false,
  "publisher": {
     "id": "canonical",
     "name": "canonical",
     "title": "Canonical"
  },
  "revision": 3887,
  "snap-id": "99T7MUlRhtI3U0QFgl5mXXESAiSwt776",
  "summary": "snapd runtime environment",
  "title": "core",
  "type": "os",
  "version": "16-2.30",
  "media": []
}`

	thingyStoreJSON = `{
  "architectures": [
    "amd64"
  ],
  "base": "base-18",
  "confinement": "strict",
  "contact": "https://thingy.com",
  "created-at": "2018-01-26T11:38:35.536410+00:00",
  "description": "Useful thingy for thinging",
  "download": {
     "sha3-384": "a29f8d894c92ad19bb943764eb845c6bd7300f555ee9b9dbb460599fecf712775c0f3e2117b5c56b08fcb9d78fc8ae4d",
     "size": 10000021,
     "url": "https://api.snapcraft.io/api/v1/snaps/download/XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap",
     "deltas": [
       {
         "format": "xdelta3",
         "source": 19,
         "target": 21,
         "url": "https://api.snapcraft.io/api/v1/snaps/download/XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV_19_21_xdelta3.delta",
         "size": 9999,
         "sha3-384": "29f8d894c92ad19bb943764eb845c6bd7300f555ee9b9dbb460599fecf712775c0f3e2117b5c56b08fcb9d78fc8ae4df"
       }
     ]
  },
  "epoch": {
     "read": [0,1],
     "write": [1]
  },
  "license": "Proprietary",
  "name": "thingy",
  "prices": {"USD": "9.99"},
  "private": false,
  "publisher": {
     "id": "ZvtzsxbsHivZLdvzrt0iqW529riGLfXJ",
     "name": "thingyinc",
     "title": "Thingy Inc."
  },
  "revision": 21,
  "snap-id": "XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV",
  "snap-yaml": "name: test-snapd-content-plug\nversion: 1.0\napps:\n    content-plug:\n        command: bin/content-plug\n        plugs: [shared-content-plug]\nplugs:\n    shared-content-plug:\n        interface: content\n        target: import\n        content: mylib\n        default-provider: test-snapd-content-slot\nslots:\n    shared-content-slot:\n        interface: content\n        content: mylib\n        read:\n            - /\n",
  "summary": "useful thingy",
  "title": "thingy",
  "type": "app",
  "version": "9.50",
  "media": [
     {"type": "icon", "url": "https://dashboard.snapcraft.io/site_media/appmedia/2017/12/Thingy.png"},
     {"type": "screenshot", "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_01.png"},
     {"type": "screenshot", "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_02.png", "width": 600, "height": 200}
  ]
}`
)

func (s *detailsV2Suite) TestInfoFromStoreSnapSimple(c *C) {
	var snp storeSnap
	err := json.Unmarshal([]byte(coreStoreJSON), &snp)
	c.Assert(err, IsNil)

	info, err := infoFromStoreSnap(&snp)
	c.Assert(err, IsNil)
	c.Check(snap.Validate(info), IsNil)

	c.Check(info, DeepEquals, &snap.Info{
		Architectures: []string{"amd64"},
		SideInfo: snap.SideInfo{
			RealName:          "core",
			SnapID:            "99T7MUlRhtI3U0QFgl5mXXESAiSwt776",
			Revision:          snap.R(3887),
			Contact:           "mailto:snappy-canonical-storeaccount@canonical.com",
			EditedTitle:       "core",
			EditedSummary:     "snapd runtime environment",
			EditedDescription: "The core runtime environment for snapd",
			Private:           false,
			Paid:              false,
		},
		Epoch:       *snap.E("0"),
		Type:        snap.TypeOS,
		Version:     "16-2.30",
		Confinement: snap.StrictConfinement,
		PublisherID: "canonical",
		Publisher:   "canonical",
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://api.snapcraft.io/api/v1/snaps/download/99T7MUlRhtI3U0QFgl5mXXESAiSwt776_3887.snap",
			Sha3_384:    "b691f6dde3d8022e4db563840f0ef82320cb824b6292ffd027dbc838535214dac31c3512c619beaf73f1aeaf35ac62d5",
			Size:        85291008,
		},
		Plugs: make(map[string]*snap.PlugInfo),
		Slots: make(map[string]*snap.SlotInfo),
	})
}

func (s *detailsV2Suite) TestInfoFromStoreSnap(c *C) {
	var snp storeSnap
	// base, prices, media
	err := json.Unmarshal([]byte(thingyStoreJSON), &snp)
	c.Assert(err, IsNil)

	info, err := infoFromStoreSnap(&snp)
	c.Assert(err, IsNil)
	c.Check(snap.Validate(info), IsNil)

	info2 := *info
	// clear recursive bits
	info2.Plugs = nil
	info2.Slots = nil
	c.Check(&info2, DeepEquals, &snap.Info{
		Architectures: []string{"amd64"},
		Base:          "base-18",
		SideInfo: snap.SideInfo{
			RealName:          "thingy",
			SnapID:            "XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV",
			Revision:          snap.R(21),
			Contact:           "https://thingy.com",
			EditedTitle:       "thingy",
			EditedSummary:     "useful thingy",
			EditedDescription: "Useful thingy for thinging",
			Private:           false,
			Paid:              true,
		},
		Epoch: snap.Epoch{
			Read:  []uint32{0, 1},
			Write: []uint32{1},
		},
		Type:        snap.TypeApp,
		Version:     "9.50",
		Confinement: snap.StrictConfinement,
		License:     "Proprietary",
		PublisherID: "ZvtzsxbsHivZLdvzrt0iqW529riGLfXJ",
		Publisher:   "thingyinc",
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://api.snapcraft.io/api/v1/snaps/download/XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap",
			Sha3_384:    "a29f8d894c92ad19bb943764eb845c6bd7300f555ee9b9dbb460599fecf712775c0f3e2117b5c56b08fcb9d78fc8ae4d",
			Size:        10000021,
			Deltas: []snap.DeltaInfo{
				{
					Format:       "xdelta3",
					FromRevision: 19,
					ToRevision:   21,
					DownloadURL:  "https://api.snapcraft.io/api/v1/snaps/download/XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV_19_21_xdelta3.delta",
					Size:         9999,
					Sha3_384:     "29f8d894c92ad19bb943764eb845c6bd7300f555ee9b9dbb460599fecf712775c0f3e2117b5c56b08fcb9d78fc8ae4df",
				},
			},
		},
		Prices: map[string]float64{
			"USD": 9.99,
		},
		IconURL: "https://dashboard.snapcraft.io/site_media/appmedia/2017/12/Thingy.png",
		Screenshots: []snap.ScreenshotInfo{
			{URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_01.png"},
			{URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_02.png", Width: 600, Height: 200},
		},
	})

	// validate the plugs/slots
	c.Assert(info.Plugs, HasLen, 1)
	plug := info.Plugs["shared-content-plug"]
	c.Check(plug.Name, Equals, "shared-content-plug")
	c.Check(plug.Snap, Equals, info)
	c.Check(plug.Apps, HasLen, 1)
	c.Check(plug.Apps["content-plug"].Command, Equals, "bin/content-plug")

	c.Assert(info.Slots, HasLen, 1)
	slot := info.Slots["shared-content-slot"]
	c.Check(slot.Name, Equals, "shared-content-slot")
	c.Check(slot.Snap, Equals, info)
	c.Check(slot.Apps, HasLen, 1)
	c.Check(slot.Apps["content-plug"].Command, Equals, "bin/content-plug")

	// private
	err = json.Unmarshal([]byte(strings.Replace(thingyStoreJSON, `"private": false`, `"private": true`, 1)), &snp)
	c.Assert(err, IsNil)

	info, err = infoFromStoreSnap(&snp)
	c.Assert(err, IsNil)
	c.Check(snap.Validate(info), IsNil)

	c.Check(info.Private, Equals, true)
}
