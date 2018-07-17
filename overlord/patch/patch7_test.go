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

package patch_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type patch7Suite struct{}

var _ = Suite(&patch7Suite{})

var statePatch7JSON = []byte(`
{
	"last-task-id": 999,
	"last-change-id": 99,

	"data": {
		"patch-level": 6,
		"snaps": {
			"core": {
				"sequence": [{"name": "core", "revision": "2"}],
                                "flags": 1,
				"current": "2"}
		},
	    "conns": {
	      "gnome-calculator:icon-themes gtk-common-themes:icon-themes": {
		  "auto": true,
		  "interface": "content",
		  "plug-static": {
		    "content": "icon-themes",
		    "default-provider": "gtk-common-themes",
		    "target": "$SNAP/data-dir/icons"
		  },
		  "slot-static": {
		    "content": "icon-themes",
		    "source": {
		      "read": ["$SNAP/share/icons/Adwaita"]
		    }
		  }
		},
                "foobar:fooplug gtk-common-themes:icon-themes": {
		  "auto": true,
		  "interface": "content"
		},
		"gnome-calculator:network core:network": {
		  "auto": true,
		  "interface": "network"
		},
                "other-snap:icon-themes gtk-common-themes:icon-themes": {
		  "auto": true,
		  "interface": "content",
                  "undesired":true
		}
	    }
	},
	"changes": {
		"1": {
			"id": "1",
			"kind": "install-snap",
			"summary": "install a snap",
			"status": 0,
			"data": {"snap-names": ["foobar"]},
			"task-ids": ["11"]
		}
	},
	"tasks": {
                "11": {
                        "id": "11",
                        "change": "1",
                        "kind": "discard-conns",
                        "summary": "",
                        "status": 0,
                        "data": {"snap-setup": {
                                "channel": "edge",
                                "flags": 1
				},
				"removed": {
				    "foobar:fooplug gtk-common-themes:icon-themes": {
				      "auto": true,
				      "interface": "content"
				    }
				}
			},
                        "halt-tasks": []
                }
	}
}
`)

var mockSnap1Yaml = `
name: gnome-calculator
version: 3.28.2
summary: GNOME Calculator
grade: stable
plugs:
  icon-themes:
    default-provider: gtk-common-themes
    interface: content
    target: $SNAP/data-dir/icons
  sound-themes:
    default-provider: gtk-common-themes
    interface: content
    target: $SNAP/data-dir/sounds
slots:
  gnome-calculator:
    bus: session
    interface: dbus
    name: org.gnome.Calculator
apps:
  gnome-calculator:
    command: command-gnome-calculator.wrapper
`

var mockSnap2Yaml = `
name: foobar
version: 1
summary: FooBar snap
plugs:
  fooplug:
    interface: content
    target: $SNAP/foo-dir/icons
slots:
  fooslot:
    bus: session
    interface: dbus
    name: org.gnome.Calculator
apps:
  foo:
    command: bar
`

var mockSnap3Yaml = `
name: gtk-common-themes
version: 1
summary: gtk common themes snap
slots:
  icon-themes:
    bus: session
    interface: dbus
    name: org.gnome.Calculator
apps:
  foo:
    command: bar
`

var mockSnap4Yaml = `
name: other-snap
version: 1.1
plugs:
  icon-themes:
    default-provider: gtk-common-themes
    interface: content
    target: $SNAP/data-dir/icons
apps:
  foo:
    command: bar
`

func (s *patch7Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch7JSON, 0644)
	c.Assert(err, IsNil)

	snap.MockSanitizePlugsSlots(func(*snap.Info) {})

	snaptest.MockSnapCurrent(c, mockSnap1Yaml, &snap.SideInfo{Revision: snap.R("x1")})
	snaptest.MockSnapCurrent(c, mockSnap2Yaml, &snap.SideInfo{Revision: snap.R("x1")})
	snaptest.MockSnapCurrent(c, mockSnap3Yaml, &snap.SideInfo{Revision: snap.R("x1")})
	snaptest.MockSnapCurrent(c, mockSnap4Yaml, &snap.SideInfo{Revision: snap.R("x1")})
}

func (s *patch7Suite) TestPatch7(c *C) {
	restore := patch.MockLevel(7)
	defer restore()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	c.Assert(patch.Apply(st), IsNil)

	st.Lock()
	defer st.Unlock()

	var conns map[string]interface{}
	c.Assert(st.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"gnome-calculator:icon-themes gtk-common-themes:icon-themes": map[string]interface{}{
			"auto":      true,
			"interface": "content",
			"plug-static": map[string]interface{}{
				"content":          "icon-themes",
				"default-provider": "gtk-common-themes",
				"target":           "$SNAP/data-dir/icons",
			},
			"slot-static": map[string]interface{}{
				"content": "icon-themes",
				"source": map[string]interface{}{
					"read": []interface{}{"$SNAP/share/icons/Adwaita"},
				},
			},
		},
		"foobar:fooplug gtk-common-themes:icon-themes": map[string]interface{}{
			"auto":      true,
			"interface": "content",
			"plug-static": map[string]interface{}{
				"target": "$SNAP/foo-dir/icons",
			},
			"slot-static": map[string]interface{}{
				"bus":  "session",
				"name": "org.gnome.Calculator",
			},
		},
		"gnome-calculator:network core:network": map[string]interface{}{
			"auto":      true,
			"interface": "network",
		},
		"other-snap:icon-themes gtk-common-themes:icon-themes": map[string]interface{}{
			"undesired": true,
			"auto":      true,
			"interface": "content",
		},
	})

	var removed map[string]interface{}
	task := st.Task("11")
	c.Assert(task, NotNil)
	c.Assert(task.Get("removed", &removed), IsNil)
	c.Assert(removed, DeepEquals, map[string]interface{}{
		"foobar:fooplug gtk-common-themes:icon-themes": map[string]interface{}{
			"auto":      true,
			"interface": "content",
			"plug-static": map[string]interface{}{
				"target": "$SNAP/foo-dir/icons",
			},
			"slot-static": map[string]interface{}{
				"bus":  "session",
				"name": "org.gnome.Calculator",
			},
		},
	})
}
