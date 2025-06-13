// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	snapcli "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/check.v1"
)

func (s *SnapSuite) TestComponentsHelp(c *check.C) {
	msg := `Usage:
  snap.test components [<snap>...]

The components command displays a summary of the components that are installed
and available for the set of currently installed snaps.

Components for specific installed snaps can be queried by providing snap names
as positional arguments.

[components command arguments]
  <snap>:         Snaps to consider when listing available and installed
                  components.
`
	s.testSubCommandHelp(c, "components", msg)
}

type testComponentOpts struct {
	err       error
	stderr    string
	stdout    string
	provided  []string
	installed []client.Snap
}

func (s *SnapSuite) TestComponents(c *check.C) {
	s.testComponents(c, testComponentOpts{
		stdout: `Component      Status     Type
snap-1+comp-1  installed  standard
snap-1+comp-3  installed  standard
snap-1+comp-2  available  kernel-modules
snap-2+comp-2  available  standard
`,
		installed: []client.Snap{
			{
				Name: "snap-2",
				Components: []client.Component{
					{
						Name: "comp-2",
						Type: snap.StandardComponent,
					},
				},
			},
			{
				Name: "snap-1",
				Components: []client.Component{
					{
						Name:        "comp-1",
						Type:        snap.StandardComponent,
						InstallDate: &time.Time{},
					},
					{
						Name: "comp-2",
						Type: snap.KernelModulesComponent,
					},
					{
						Name:        "comp-3",
						Type:        snap.StandardComponent,
						InstallDate: &time.Time{},
					},
				},
			},
		},
	})
}

func (s *SnapSuite) TestComponentsNoComponents(c *check.C) {
	s.testComponents(c, testComponentOpts{
		stderr: "No components are available for any installed snaps.\n",
		installed: []client.Snap{
			{
				Name: "snap-2",
			},
			{
				Name: "snap-1",
			},
		},
	})
}

func (s *SnapSuite) TestComponentsInstanceName(c *check.C) {
	s.testComponents(c, testComponentOpts{
		stdout: `Component          Status     Type
snap-1_one+comp-1  installed  standard
snap-1_one+comp-2  available  kernel-modules
snap-1_two+comp-2  installed  kernel-modules
snap-1_two+comp-1  available  standard
`,
		installed: []client.Snap{
			{
				Name: "snap-1_one",
				Components: []client.Component{
					{
						Name:        "comp-1",
						Type:        snap.StandardComponent,
						InstallDate: &time.Time{},
					},
					{
						Name: "comp-2",
						Type: snap.KernelModulesComponent,
					},
				},
			},
			{
				Name: "snap-1_two",
				Components: []client.Component{
					{
						Name: "comp-1",
						Type: snap.StandardComponent,
					},
					{
						Name:        "comp-2",
						Type:        snap.KernelModulesComponent,
						InstallDate: &time.Time{},
					},
				},
			},
		},
	})
}

func (s *SnapSuite) TestComponentsNoSnaps(c *check.C) {
	s.testComponents(c, testComponentOpts{
		stderr: "No snaps are installed yet.\n",
	})
}

func (s *SnapSuite) TestComponentsFiltered(c *check.C) {
	s.testComponents(c, testComponentOpts{
		stdout: `Component      Status     Type
snap-2+comp-2  available  standard
`,
		installed: []client.Snap{
			{
				Name: "snap-2",
				Components: []client.Component{
					{
						Name: "comp-2",
						Type: snap.StandardComponent,
					},
				},
			},
		},
		provided: []string{"snap-2"},
	})
}

func (s *SnapSuite) TestComponentsNoMatchingSnaps(c *check.C) {
	s.testComponents(c, testComponentOpts{
		err:      snapcli.ErrNoMatchingSnaps,
		provided: []string{"snap-2"},
	})
}

func (s *SnapSuite) testComponents(c *check.C, opts testComponentOpts) {
	called := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if called > 0 {
			c.Fatalf("expected to get 1 request, now on %d", called)
		}
		called++

		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		if len(opts.provided) == 0 {
			c.Check(r.URL.RawQuery, check.Equals, "")
		} else {
			c.Check(r.URL.RawQuery, check.Equals, fmt.Sprintf("snaps=%s", strings.Join(opts.provided, ",")))
		}

		response := map[string]any{
			"type":   "sync",
			"result": opts.installed,
		}

		err := json.NewEncoder(w).Encode(response)
		c.Assert(err, check.IsNil)
	})

	args := append([]string{"components"}, opts.provided...)
	rest, err := snapcli.Parser(snapcli.Client()).ParseArgs(args)
	if opts.err != nil {
		c.Assert(err, testutil.ErrorIs, opts.err)
	} else {
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.HasLen, 0)
	}

	c.Check(s.Stdout(), check.Equals, opts.stdout)
	c.Check(s.Stderr(), check.Equals, opts.stderr)
}
