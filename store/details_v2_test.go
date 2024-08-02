// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2023 Canonical Ltd
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

// not using store_test as this is a very low level test
package store

import (
	"encoding/json"
	"reflect"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/jsonutil/safejson"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type detailsV2Suite struct {
	testutil.BaseTest
}

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
     "username": "canonical",
     "display-name": "Canonical",
     "validation": "verified"
  },
  "revision": 3887,
  "snap-id": "99T7MUlRhtI3U0QFgl5mXXESAiSwt776",
  "store-url": "https://snapcraft.io/core",
  "summary": "snapd runtime environment",
  "title": "core",
  "type": "os",
  "version": "16-2.30",
  "website": "http://example.com/core",
  "media": [],
  "resources": [
    {
      "download": {
        "sha3-384": "6d001da919b965dc3a4672b9d7ddce374d165452a2285f2753988842092ea6b9946645375cff3ede89a991c9698bfcea",
        "size": 20000021,
        "url": "https://api.snapcraft.io/api/v1/snaps/download/ABCEfjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap"
      },
      "type": "component/test",
      "name": "some-component",
      "revision": 1,
      "version": "1.0",
      "created-at": "2018-01-26T11:38:35.536410+00:00",
      "description": "Some component"
    },
    {
      "download": {
        "sha3-384": "6d001da919b965dc3a4672b9d7ddce374d165452a2285f2753988842092ea6b9946645375cff3ede89a991c9698bfcea",
        "size": 20000021,
        "url": "https://api.snapcraft.io/api/v1/snaps/download/ABCEfjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap"
      },
      "type": "component/unknown-type",
      "name": "unknown-component",
      "revision": 1,
      "version": "1.0",
      "created-at": "2018-01-26T11:38:35.536410+00:00",
      "description": "Unknown component"
    },
    {
      "download": {
        "sha3-384": "e6da7b15f767111ce34f22fa2059d23b43cb756e73256279e1d7f98a2eaab0d93725c2bfb25dd1deb1261223d961ee61",
        "size": 20000023,
        "url": "https://api.snapcraft.io/api/v1/snaps/download/123Efjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap"
      },
      "type": "not-a-component/thing",
      "name": "some-not-component",
      "revision": 1,
      "version": "1.0",
      "created-at": "2018-01-26T11:38:35.536410+00:00",
      "description": "Something that is not a component"
    }
  ]
}`

	thingyStoreJSON = `{
  "architectures": [
    "amd64"
  ],
  "base": "base-18",
  "categories": [
    {"featured": true, "name": "featured"},
    {"featured": false, "name": "productivity"}
  ],
  "confinement": "strict",
  "contact": "https://thingy.com",
  "common-ids": ["org.thingy"],
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
     "username": "thingyinc",
     "display-name": "Thingy Inc.",
     "validation": "unproven"
  },
  "links": {
    "contact": ["https://thingy.com","mailto:thingy@thingy.com"],
    "website": ["http://example.com/thingy"],
    "issues": ["mailto:bugs@thingy.com"],
    "empty": []
  },
  "revision": 21,
  "snap-id": "XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV",
  "snap-yaml": "name: test-snapd-content-plug\nversion: 1.0\nassumes: [snapd2.49]\napps:\n    user-svc:\n        command: bin/user-svc\n        daemon-scope: user\n        daemon: simple\n    content-plug:\n        command: bin/content-plug\n        plugs: [shared-content-plug]\nplugs:\n    shared-content-plug:\n        interface: content\n        target: import\n        content: mylib\n        default-provider: test-snapd-content-slot\nslots:\n    shared-content-slot:\n        interface: content\n        content: mylib\n        read:\n            - /\nprovenance: prov\ncomponents:\n  some-component:\n    type: test\n    name: some-component\n    description: Some component\n    summary: Component summary\n    hooks:\n      install:",
  "store-url": "https://snapcraft.io/thingy",
  "summary": "useful thingy",
  "title": "This Is The Most Fantastical Snap of Thingy",
  "type": "app",
  "version": "9.50",
  "website": "http://example.com/thingy",
  "media": [
     {"type": "icon", "url": "https://dashboard.snapcraft.io/site_media/appmedia/2017/12/Thingy.png"},
     {"type": "screenshot", "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_01.png"},
     {"type": "screenshot", "url": "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_02.png", "width": 600, "height": 200}
  ],
  "resources": [
    {
      "download": {
          "sha3-384": "6d001da919b965dc3a4672b9d7ddce374d165452a2285f2753988842092ea6b9946645375cff3ede89a991c9698bfcea",
          "size": 20000021,
          "url": "https://api.snapcraft.io/api/v1/snaps/download/ABCEfjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap"
      },
      "type": "component/test",
      "name": "some-component",
      "revision": 1,
      "version": "1.0",
      "created-at": "2018-01-26T11:38:35.536410+00:00",
      "description": "Some component"
    },
    {
      "download": {
          "sha3-384": "e6da7b15f767111ce34f22fa2059d23b43cb756e73256279e1d7f98a2eaab0d93725c2bfb25dd1deb1261223d961ee61",
          "size": 20000023,
          "url": "https://api.snapcraft.io/api/v1/snaps/download/123Efjn4WJYnm0FzDKwqqRZZI77awQEV_21.snap"
      },
      "type": "not-a-component/thing",
      "name": "some-not-component",
      "revision": 1,
      "version": "1.0",
      "created-at": "2018-01-26T11:38:35.536410+00:00",
      "description": "Something that is not a component"
    }
  ]
}`
)

func (s *detailsV2Suite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *detailsV2Suite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *detailsV2Suite) TestInfoFromStoreSnapSimpleAndLegacy(c *C) {
	var snp storeSnap
	err := json.Unmarshal([]byte(coreStoreJSON), &snp)
	c.Assert(err, IsNil)

	info, err := infoFromStoreSnap(&snp)
	c.Assert(err, IsNil)
	c.Check(snap.Validate(info), IsNil)

	c.Check(info, DeepEquals, &snap.Info{
		Architectures: []string{"amd64"},
		SideInfo: snap.SideInfo{
			RealName:            "core",
			SnapID:              "99T7MUlRhtI3U0QFgl5mXXESAiSwt776",
			Revision:            snap.R(3887),
			LegacyEditedContact: "mailto:snappy-canonical-storeaccount@canonical.com",
			EditedTitle:         "core",
			EditedSummary:       "snapd runtime environment",
			EditedDescription:   "The core runtime environment for snapd",
			Private:             false,
			Paid:                false,
		},
		Epoch:       snap.E("0"),
		SnapType:    snap.TypeOS,
		Version:     "16-2.30",
		Confinement: snap.StrictConfinement,
		Publisher: snap.StoreAccount{
			ID:          "canonical",
			Username:    "canonical",
			DisplayName: "Canonical",
			Validation:  "verified",
		},
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://api.snapcraft.io/api/v1/snaps/download/99T7MUlRhtI3U0QFgl5mXXESAiSwt776_3887.snap",
			Sha3_384:    "b691f6dde3d8022e4db563840f0ef82320cb824b6292ffd027dbc838535214dac31c3512c619beaf73f1aeaf35ac62d5",
			Size:        85291008,
		},
		LegacyWebsite: "http://example.com/core",
		StoreURL:      "https://snapcraft.io/core",

		// components are derived from resources in this case, rather than
		// snap-yaml. note that non-component resources are ignored and unknown
		// components types are ignored
		Components: map[string]*snap.Component{
			"some-component": {
				Name:        "some-component",
				Type:        snap.TestComponent,
				Description: "Some component",
			},
		},
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
	c.Check(snap.ValidateLinks(info.EditedLinks), IsNil)

	info2 := *info
	// clear recursive bits
	info2.Plugs = nil
	info2.Slots = nil
	info2.Apps = nil
	info2.Hooks = nil
	info2.Components = nil
	c.Check(&info2, DeepEquals, &snap.Info{
		SuggestedName: "test-snapd-content-plug",
		Architectures: []string{"amd64"},
		Assumes:       []string{"snapd2.49"},
		Base:          "base-18",
		SideInfo: snap.SideInfo{
			RealName: "thingy",
			SnapID:   "XYZEfjn4WJYnm0FzDKwqqRZZI77awQEV",
			Revision: snap.R(21),
			EditedLinks: map[string][]string{
				"contact": {"https://thingy.com", "mailto:thingy@thingy.com"},
				"website": {"http://example.com/thingy"},
				"issues":  {"mailto:bugs@thingy.com"},
			},
			LegacyEditedContact: "https://thingy.com",
			EditedTitle:         "This Is The Most Fantastical Snap of Th…",
			EditedSummary:       "useful thingy",
			EditedDescription:   "Useful thingy for thinging",
			Private:             false,
			Paid:                true,
		},
		Epoch: snap.Epoch{
			Read:  []uint32{0, 1},
			Write: []uint32{1},
		},
		SnapType:    snap.TypeApp,
		Version:     "9.50",
		Confinement: snap.StrictConfinement,
		License:     "Proprietary",
		Publisher: snap.StoreAccount{
			ID:          "ZvtzsxbsHivZLdvzrt0iqW529riGLfXJ",
			Username:    "thingyinc",
			DisplayName: "Thingy Inc.",
			Validation:  "unproven",
		},
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
		Media: []snap.MediaInfo{
			{Type: "icon", URL: "https://dashboard.snapcraft.io/site_media/appmedia/2017/12/Thingy.png"},
			{Type: "screenshot", URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_01.png"},
			{Type: "screenshot", URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_02.png", Width: 600, Height: 200},
		},
		CommonIDs: []string{"org.thingy"},
		Categories: []snap.CategoryInfo{
			{Featured: true, Name: "featured"},
			{Featured: false, Name: "productivity"},
		},
		StoreURL:       "https://snapcraft.io/thingy",
		SnapProvenance: "prov",
		// empty
		BadInterfaces:   map[string]string{},
		SystemUsernames: map[string]*snap.SystemUsernameInfo{},
		OriginalLinks:   map[string][]string{},
		LegacyAliases:   map[string]*snap.AppInfo{},
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
	c.Check(slot.Apps, HasLen, 2)
	c.Check(slot.Apps["content-plug"].Command, Equals, "bin/content-plug")

	// validate apps
	c.Check(info.Apps["user-svc"].Command, Equals, "bin/user-svc")
	c.Check(info.Apps["user-svc"].Daemon, Equals, "simple")
	c.Check(info.Apps["user-svc"].DaemonScope, Equals, snap.UserDaemon)

	// validate components
	someComponent := *info.Components["some-component"]
	c.Assert(someComponent, NotNil)

	c.Check(someComponent.ExplicitHooks["install"].Explicit, Equals, true)
	// clear recursive bits
	someComponent.ExplicitHooks = nil

	c.Check(someComponent, DeepEquals, snap.Component{
		Name:        "some-component",
		Type:        snap.TestComponent,
		Description: "Some component",
		Summary:     "Component summary",
	})

	// private
	err = json.Unmarshal([]byte(strings.Replace(thingyStoreJSON, `"private": false`, `"private": true`, 1)), &snp)
	c.Assert(err, IsNil)

	info, err = infoFromStoreSnap(&snp)
	c.Assert(err, IsNil)
	c.Check(snap.Validate(info), IsNil)

	c.Check(info.Private, Equals, true)

	// check that up to few exceptions info is filled
	expectedZeroFields := []string{
		"InstanceKey",
		"OriginalTitle",
		"OriginalSummary",
		"OriginalDescription",
		"Environment",
		"LicenseAgreement", // XXX go away?
		"LicenseVersion",   // XXX go away?
		"Broken",
		"MustBuy",
		"Channels", // handled at a different level (see TestInfo)
		"Tracks",   // handled at a different level (see TestInfo)
		"Layout",
		"SideInfo.Channel",
		"LegacyWebsite",
	}
	var checker func(string, reflect.Value)
	checker = func(pfx string, x reflect.Value) {
		t := x.Type()
		for i := 0; i < x.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				// not exported, ignore
				continue
			}
			v := x.Field(i)
			if f.Anonymous {
				checker(pfx+f.Name+".", v)
				continue
			}
			name := pfx + f.Name
			if reflect.DeepEqual(v.Interface(), reflect.Zero(f.Type).Interface()) {
				c.Check(expectedZeroFields, testutil.Contains, name, Commentf("%s not set", name))
			} else {
				c.Check(expectedZeroFields, Not(testutil.Contains), name, Commentf("%s unexpectedly set", name))
			}
		}
	}
	x := reflect.ValueOf(info).Elem()
	checker("", x)
}

// arg must be a pointer to a struct
func fillStruct(a interface{}, c *C) {
	if t := reflect.TypeOf(a); t.Kind() != reflect.Ptr || t.Elem().Kind() != reflect.Struct {
		k := t.Kind()
		if k == reflect.Ptr {
			k = t.Elem().Kind()
		}
		c.Fatalf("first argument must be expected a pointer to a struct, not %s", k)
	}
	va := reflect.ValueOf(a)
	n := va.Elem().NumField()
	for i := 0; i < n; i++ {
		field := va.Elem().Field(i)
		var x interface{}
		switch field.Interface().(type) {
		case string:
			x = "foo"
		case []string:
			x = []string{"foo"}
		case safejson.String:
			var s safejson.String
			c.Assert(json.Unmarshal([]byte(`"foo"`), &s), IsNil)
			x = s
		case safejson.Paragraph:
			var p safejson.Paragraph
			c.Assert(json.Unmarshal([]byte(`"foo"`), &p), IsNil)
			x = p
		case storeDownload:
			x = storeDownload{
				URL:      "http://example.com/foo",
				Size:     42,
				Sha3_384: "foo",
			}
		case snap.Epoch:
			x = snap.E("1")
		case map[string]string:
			x = map[string]string{"foo": "bar"}
		case bool:
			x = true
		case snap.StoreAccount:
			x = snap.StoreAccount{
				ID:          "foo-id",
				Username:    "foo",
				DisplayName: "Foo Bar",
				Validation:  "VALIDATION",
			}
		case int:
			x = 42
		case snap.Type:
			x = snap.Type("invalid")
		case []storeSnapMedia:
			x = []storeSnapMedia{{
				Type: "potato",
				URL:  "http://example.com/foo.pot",
			}}
		case []storeSnapCategory:
			x = []storeSnapCategory{{
				Featured: false,
				Name:     "foo",
			}}
		case map[string][]string:
			x = map[string][]string{
				"contact": {"mailto:foo", "mailto:bar"},
			}
		case []storeResource:
			x = []storeResource{{
				Type: "component/test",
				Download: storeDownload{
					URL:      "http://example.com/resource",
					Size:     42,
					Sha3_384: "sha",
				},
				Name:     "some-component",
				Revision: 1,
			}}
		default:
			c.Fatalf("unhandled field type %T", field.Interface())
		}
		field.Set(reflect.ValueOf(x))
	}
}

func (s *detailsV2Suite) TestCopyNonZero(c *C) {
	// a is a storeSnap with everything non-zero
	a := storeSnap{}
	fillStruct(&a, c)
	// b is all zeros
	b := storeSnap{}

	aCopy := a
	bCopy := b

	// validity check
	c.Check(a, DeepEquals, aCopy)
	c.Check(b, DeepEquals, bCopy)
	c.Check(a, Not(DeepEquals), b)

	// copying from b to a does nothing:
	copyNonZeroFrom(&b, &a)
	c.Check(a, DeepEquals, aCopy)
	c.Check(b, DeepEquals, bCopy)

	// copying from a to b does its thing:
	copyNonZeroFrom(&a, &b)
	c.Check(a, DeepEquals, b)
	c.Check(b, Not(DeepEquals), bCopy)
}
