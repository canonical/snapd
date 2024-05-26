// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	main "github.com/snapcore/snapd/cmd/snap"
)

var stateJSON = []byte(`
{
	"last-task-id": 31,
	"last-change-id": 10,

	"data": {
		"snaps": {},
		"seeded": true
	},
	"changes": {
		"9": {
			"id": "9",
			"kind": "install-snap",
			"summary": "install a snap",
			"status": 0,
			"data": {"snap-names": ["a"]},
			"task-ids": ["11","12"],
                        "spawn-time": "2009-11-10T23:00:00Z"
		},
		"10": {
			"id": "10",
			"kind": "revert-snap",
			"summary": "revert c snap",
			"status": 0,
			"data": {"snap-names": ["c"]},
			"task-ids": ["21","31"],
                        "spawn-time": "2009-11-10T23:00:10Z",
                        "ready-time": "2009-11-10T23:00:30Z"
		}
	},
	"tasks": {
		"11": {
				"id": "11",
				"change": "9",
				"kind": "download-snap",
				"summary": "Download snap a from channel edge",
				"status": 4,
				"data": {"snap-setup": {
						"channel": "edge",
						"flags": 1
				}},
				"halt-tasks": ["12"]
		},
		"12": {"id": "12", "change": "9", "kind": "some-other-task"},
		"21": {
				"id": "21",
				"change": "10",
				"kind": "download-snap",
				"summary": "Download snap b from channel beta",
				"status": 4,
				"data": {"snap-setup": {
						"channel": "beta",
						"flags": 2
				}},
				"halt-tasks": ["12"]
		},
		"31": {
				"id": "31",
				"change": "10",
				"kind": "prepare-snap",
				"summary": "Prepare snap c",
				"status": 4,
				"data": {"snap-setup": {
						"channel": "stable",
						"flags": 1073741828
				}},
				"halt-tasks": ["12"],
				"log": ["logline1", "logline2"]
		}
	}
}
`)

var stateConnsJSON = []byte(`
{
	"data": {
		"conns": {
			"gnome-calculator:desktop-legacy core:desktop-legacy": {
				"auto": true,
				"interface": "desktop-legacy"
			},
			"gnome-calculator:gtk-3-themes gtk-common-themes:gtk-3-themes": {
				"auto": true,
				"interface": "content",
				"plug-static": {
					"content": "gtk-3-themes",
					"default-provider": "gtk-common-themes",
					"target": "$SNAP/data-dir/themes"
				},
				"slot-static": {
					"content": "gtk-3-themes",
					"source": {
					"read": [
						"$SNAP/share/themes/Adwaita",
						"$SNAP/share/themes/Materia-light-compact"
					]
					}
				}
			},
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
					"read": [
						"$SNAP/share/icons/Adwaita",
						"$SNAP/share/icons/elementary-xfce-darkest"
					]
					}
				}
			},
			"gnome-calculator:network core:network": {
				"auto": true,
				"interface": "network"
			},
			"gnome-calculator:x11 core:x11": {
				"auto": true,
				"interface": "x11"
			},
			"vlc:x11 core:x11": {
				"auto": true,
				"interface": "x11"
			},
			"vlc:network core:network": {
				"auto": true,
				"undesired": true,
				"interface": "network"
			},
			"some-snap:network core:network": {
				"auto": true,
				"by-gadget": true,
				"interface": "network"
			}
		}
	}
}`)

var stateCyclesJSON = []byte(`
{
	"last-task-id": 14,
	"last-change-id": 2,

	"data": {
		"snaps": {},
		"seeded": true
	},
	"changes": {
		"1": {
			"id": "1",
			"kind": "install-snap",
			"summary": "install a snap",
			"status": 0,
			"task-ids": ["11","12","13"]
		}
	},
	"tasks": {
		"11": {
			"id": "11",
			"change": "1",
			"kind": "foo",
			"summary": "Foo task",
			"status": 4,
			"halt-tasks": ["13"],
			"lanes": [1,2]
		},
		"12": {
			"id": "12",
			"change": "1",
			"kind": "bar",
			"summary": "Bar task",
			"halt-tasks": ["13"],
			"lanes": [1]
		},
		"13": {
			"id": "13",
			"change": "1",
			"kind": "bar",
			"summary": "Bar task",
			"halt-tasks": ["11","12"],
			"lanes": [2]
		}
	}
}
`)

func (s *SnapSuite) TestDebugChanges(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--abs-time", "--changes", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"ID   Status  Spawn                 Ready                 Label         Summary\n"+
			"9    Do      2009-11-10T23:00:00Z  0001-01-01T00:00:00Z  install-snap  install a snap\n"+
			"10   Done    2009-11-10T23:00:10Z  2009-11-10T23:00:30Z  revert-snap   revert c snap\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugChangesMissingState(c *C) {
	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--changes", "/missing-state.json"}))
	c.Check(err, ErrorMatches, "cannot read the state file: open /missing-state.json: no such file or directory")
}

func (s *SnapSuite) TestDebugTask(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--task=31", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "id: 31\n"+
		"kind: prepare-snap\n"+
		"summary: Prepare snap c\n"+
		"status: Done\n"+
		"log: |\n"+
		"  logline1\n"+
		"  logline2\n"+
		"\n"+
		"halt-tasks:\n"+
		" - some-other-task (12)\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugTaskEmptyLists(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--task=12", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "id: 12\n"+
		"kind: some-other-task\n"+
		"summary: \n"+
		"status: Do\n"+
		"halt-tasks: []\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugTaskMissingState(c *C) {
	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--task=1", "/missing-state.json"}))
	c.Check(err, ErrorMatches, "cannot read the state file: open /missing-state.json: no such file or directory")
}

func (s *SnapSuite) TestDebugTaskNoSuchTaskError(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--task=99", stateFile}))
	c.Check(err, ErrorMatches, "no such task: 99")
}

func (s *SnapSuite) TestDebugTaskMutuallyExclusiveCommands(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--task=99", "--changes", stateFile}))
	c.Check(err, ErrorMatches, "cannot use --changes and --task= together")

	_ = mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--changes", "--change=1", stateFile}))
	c.Check(err, ErrorMatches, "cannot use --changes and --change= together")

	_ = mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--change=1", "--task=1", stateFile}))
	c.Check(err, ErrorMatches, "cannot use --change= and --task= together")

	_ = mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--change=1", "--is-seeded", stateFile}))
	c.Check(err, ErrorMatches, "cannot use --change= and --is-seeded together")
}

func (s *SnapSuite) TestDebugTasks(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--abs-time", "--change=9", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"Lanes  ID   Status  Spawn                 Ready                 Kind             Summary\n"+
			"0      11   Done    0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  download-snap    Download snap a from channel edge\n"+
			"0      12   Do      0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  some-other-task  \n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugTasksWithCycles(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateCyclesJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--abs-time", "--change=1", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		""+
			"Lanes  ID   Status  Spawn                 Ready                 Kind  Summary\n"+
			"1      12   Do      0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  bar   Bar task\n"+
			"1,2    11   Done    0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  foo   Foo task\n"+
			"2      13   Do      0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  bar   Bar task\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugCheckForCycles(c *C) {
	// we use local time when printing times in a human-friendly format, which can
	// break the comparison below
	oldLoc := time.Local
	time.Local = time.UTC
	defer func() {
		time.Local = oldLoc
	}()

	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateCyclesJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--check", "--change=1", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, ``+
		`Detected task dependency cycle involving tasks:
Lanes  ID   Status  Spawn       Ready       Kind  Summary   After  Before
1,2    11   Done    0001-01-01  0001-01-01  foo   Foo task  []     [13]
1      12   Do      0001-01-01  0001-01-01  bar   Bar task  []     [13]
2      13   Do      0001-01-01  0001-01-01  bar   Bar task  []     [11,12]
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugTasksMissingState(c *C) {
	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--change=1", "/missing-state.json"}))
	c.Check(err, ErrorMatches, "cannot read the state file: open /missing-state.json: no such file or directory")
}

func (s *SnapSuite) TestDebugIsSeededHappy(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--is-seeded", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches, "true\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugIsSeededNo(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, []byte("{}"), 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--is-seeded", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches, "false\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugConnections(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateConnsJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--connections", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"Interface       Plug                             Slot                            Notes\n"+
			"desktop-legacy  gnome-calculator:desktop-legacy  core:desktop-legacy             auto\n"+
			"content         gnome-calculator:gtk-3-themes    gtk-common-themes:gtk-3-themes  auto\n"+
			"content         gnome-calculator:icon-themes     gtk-common-themes:icon-themes   auto\n"+
			"network         gnome-calculator:network         core:network                    auto\n"+
			"x11             gnome-calculator:x11             core:x11                        auto\n"+
			"network         some-snap:network                core:network                    auto,by-gadget\n"+
			"network         vlc:network                      core:network                    auto,undesired\n"+
			"x11             vlc:x11                          core:x11                        auto\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugConnectionDetails(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateConnsJSON, 0644), IsNil)

	for i, connArg := range []string{"gnome-calculator:gtk-3-themes", ",gtk-common-themes:gtk-3-themes"} {
		s.ResetStdStreams()
		rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", fmt.Sprintf("--connection=%s", connArg), stateFile}))

		c.Assert(rest, DeepEquals, []string{})
		c.Check(s.Stdout(), Matches,
			"id: gnome-calculator:gtk-3-themes gtk-common-themes:gtk-3-themes\n"+
				"auto: true\n"+
				"by-gadget: false\n"+
				"interface: content\n"+
				"undesired: false\n"+
				"plug-static:\n"+
				"  content: gtk-3-themes\n"+
				"  default-provider: gtk-common-themes\n"+
				"  target: \\$SNAP/data-dir/themes\n"+
				"slot-static:\n"+
				"  content: gtk-3-themes\n"+
				"  source:\n"+
				"    read:\n"+
				"    - \\$SNAP/share/themes/Adwaita\n"+
				"    - \\$SNAP/share/themes/Materia-light-compact\n"+
				"\n", Commentf("#%d: %s", i, connArg))
		c.Check(s.Stderr(), Equals, "")
	}
}

func (s *SnapSuite) TestDebugConnectionPlugAndSlot(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateConnsJSON, 0644), IsNil)

	connArg := "gnome-calculator:network,core:network"
	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", fmt.Sprintf("--connection=%s", connArg), stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"id: gnome-calculator:network core:network\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: network\n"+
			"undesired: false\n"+
			"\n", Commentf("#0: %s", connArg))
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugConnectionInvalidCombination(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateConnsJSON, 0644), IsNil)

	connArg := "gnome-calculator,core:network"
	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", fmt.Sprintf("--connection=%s", connArg), stateFile}))
	c.Assert(err, ErrorMatches, fmt.Sprintf("invalid command with connection args: %s", connArg))
	c.Check(s.Stdout(), Equals, "")
}

func (s *SnapSuite) TestDebugConnectionDetailsMany(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateConnsJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--connection=core", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"id: gnome-calculator:desktop-legacy core:desktop-legacy\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: desktop-legacy\n"+
			"undesired: false\n"+
			"\n"+
			"id: gnome-calculator:network core:network\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: network\n"+
			"undesired: false\n"+
			"\n"+
			"id: gnome-calculator:x11 core:x11\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: x11\n"+
			"undesired: false\n"+
			"\n"+
			"id: some-snap:network core:network\n"+
			"auto: true\n"+
			"by-gadget: true\n"+
			"interface: network\n"+
			"undesired: false\n"+
			"\n"+
			"id: vlc:network core:network\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: network\n"+
			"undesired: true\n"+
			"\n"+
			"id: vlc:x11 core:x11\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: x11\n"+
			"undesired: false\n"+
			"\n")

	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugConnectionDetailsManySlotSide(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(os.WriteFile(stateFile, stateConnsJSON, 0644), IsNil)

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"debug", "state", "--connection=core:x11", stateFile}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"id: gnome-calculator:x11 core:x11\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: x11\n"+
			"undesired: false\n"+
			"\n"+
			"id: vlc:x11 core:x11\n"+
			"auto: true\n"+
			"by-gadget: false\n"+
			"interface: x11\n"+
			"undesired: false\n"+
			"\n")
}
