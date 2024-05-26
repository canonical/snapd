// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

func (s *SnapSuite) TestRoutineConsoleConfStartTrivialCase(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {}}`)
		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	c.Assert(n, Equals, 1)
}

func (s *SnapSuite) TestRoutineConsoleConfStartInconsistentAPIResponseError(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			// return just refresh changes but no snap ids
			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"]
				}
			}`)
		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))
	c.Assert(err, ErrorMatches, `internal error: returned changes .* but no snap names`)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	c.Assert(n, Equals, 1)
}

func (s *SnapSuite) TestRoutineConsoleConfStartNonMaintenanceErrorReturned(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			// return internal server error
			fmt.Fprintf(w, `{
			"type":"error",
			"status-code": 500,
			"result": {
				"message": "broken server"
			}
		}`)
		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))
	c.Assert(err, ErrorMatches, "broken server")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	c.Assert(n, Equals, 1)
}

func (s *SnapSuite) TestRoutineConsoleConfStartSingleSnap(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		// first 4 times we hit the API there is a snap refresh ongoing
		case 1, 2, 3, 4:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			// return just refresh changes but no snap ids
			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["pc-kernel"]
				}
			}`)
		// 5th time we return nothing as we are done
		case 5:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {}}`)

		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "Snaps (pc-kernel) are refreshing, please wait...\n")
	c.Assert(n, Equals, 5)
}

func (s *SnapSuite) TestRoutineConsoleConfStartTwoSnaps(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		// first 4 times we hit the API there is a snap refresh ongoing
		case 1, 2, 3, 4:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			// return just refresh changes but no snap ids
			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["pc-kernel","core20"]
				}
			}`)
		// 5th time we return nothing as we are done
		case 5:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {}}`)

		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "Snaps (core20 and pc-kernel) are refreshing, please wait...\n")
	c.Assert(n, Equals, 5)
}

func (s *SnapSuite) TestRoutineConsoleConfStartMultipleSnaps(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		// first 4 times we hit the API there are snap refreshes ongoing
		case 1, 2, 3, 4:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["pc-kernel","snapd","core20","pc"]
				}
			}`)
		// 5th time we return nothing as we are done
		case 5:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {}}`)

		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "Snaps (core20, pc, pc-kernel, and snapd) are refreshing, please wait...\n")
	c.Assert(n, Equals, 5)
}

func (s *SnapSuite) TestRoutineConsoleConfStartSnapdRefreshMaintenanceJSON(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()

	// write a maintenance.json before any requests and then the first request
	// should fail and see the maintenance.json and then subsequent operations
	// succeed
	maintErr := client.Error{
		Kind:    client.ErrorKindDaemonRestart,
		Message: "daemon is restarting",
	}
	b := mylog.Check2(json.Marshal(&maintErr))

	mylog.Check(os.MkdirAll(filepath.Dir(dirs.SnapdMaintenanceFile), 0755))

	mylog.Check(os.WriteFile(dirs.SnapdMaintenanceFile, b, 0644))


	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		// 1st time we don't respond at all to simulate what happens if the user
		// triggers console-conf to start after snapd has shut down for a
		// refresh
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

		// 2nd time we hit the API, return an in-progress refresh
		case 2:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["snapd"]
				}
			}`)
		// 3rd time we are actually done
		case 3:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {}}`)

		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), testutil.Contains, "Snapd is reloading, please wait...\n")
	c.Check(s.Stderr(), testutil.Contains, "Snaps (snapd) are refreshing, please wait...\n")
	c.Assert(n, Equals, 3)
}

func (s *SnapSuite) TestRoutineConsoleConfStartSystemRebootMaintenanceJSON(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()

	r = snap.MockSnapdWaitForFullSystemReboot(0)
	defer r()

	// write a maintenance.json before any requests and then the first request
	// should fail and see the maintenance.json and then subsequent operations
	// succeed
	maintErr := client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	}
	b := mylog.Check2(json.Marshal(&maintErr))

	mylog.Check(os.MkdirAll(filepath.Dir(dirs.SnapdMaintenanceFile), 0755))

	mylog.Check(os.WriteFile(dirs.SnapdMaintenanceFile, b, 0644))


	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		// 1st time we don't respond at all to simulate what happens if the user
		// triggers console-conf to start after snapd has shut down for a
		// refresh
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))
	c.Assert(err, ErrorMatches, "system didn't reboot after 10 minutes even though snapd daemon is in maintenance")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), testutil.Contains, "System is rebooting, please wait for reboot...\n")
	c.Assert(n, Equals, 1)
}

func (s *SnapSuite) TestRoutineConsoleConfStartSnapdRefreshRestart(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {

		// 1st time we hit the API there is a snapd snap refresh ongoing
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["snapd"]
				}
			}`)

		// 2nd time we hit the API, set maintenance in the response
		case 2:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["snapd"]
				},
				"maintenance": {
					"kind": "daemon-restart",
					"message": "daemon is restarting"
				}
			}`)

		// 3rd time we return nothing as if we are down for maintenance
		case 3:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

		// 4th time we resume responding, but still in progress
		case 4:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["snapd"]
				}
			}`)

		// 5th time we are actually done
		case 5:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {}}`)

		default:
			c.Errorf("unexpected request %v", n)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), testutil.Contains, "Snapd is reloading, please wait...\n")
	c.Check(s.Stderr(), testutil.Contains, "Snaps (snapd) are refreshing, please wait...\n")
	c.Assert(n, Equals, 5)
}

func (s *SnapSuite) TestRoutineConsoleConfStartKernelRefreshReboot(c *C) {
	// make the command hit the API as fast as possible for testing
	r := snap.MockSnapdAPIInterval(0)
	defer r()
	r = snap.MockSnapdWaitForFullSystemReboot(0)
	defer r()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {

		// 1st time we hit the API there is a snapd snap refresh ongoing
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["pc-kernel"]
				}
			}`)

		// 2nd time we hit the API, set maintenance in the response, but still
		// give a valid response (so that it reads the maintenance)
		case 2:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")

			fmt.Fprintf(w, `{
				"type":"sync",
				"status-code": 200,
				"result": {
					"active-auto-refreshes": ["1"],
					"active-auto-refresh-snaps": ["pc-kernel"]
				},
				"maintenance": {
					"kind": "system-restart",
					"message": "system is restarting"
				}
			}`)

		// 3rd time we hit the API, we need to not return anything so that the
		// client will inspect the error and see there is a maintenance error
		case 3:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/internal/console-conf-start")
		default:
			c.Errorf("unexpected %s request (number %d) to %s", r.Method, n, r.URL.Path)
		}
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "console-conf-start"}))
	// this is the internal error, which we will hit immediately for testing,
	// in a real scenario a reboot would happen OOTB from the snap client
	c.Assert(err, ErrorMatches, "system didn't reboot after 10 minutes even though snapd daemon is in maintenance")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), testutil.Contains, "System is rebooting, please wait for reboot...\n")
	c.Check(s.Stderr(), testutil.Contains, "Snaps (pc-kernel) are refreshing, please wait...\n")
	c.Assert(n, Equals, 3)
}
