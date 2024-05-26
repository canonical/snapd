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
	"fmt"
	"net/http"
	"net/url"
	"os/user"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type SnapRoutineFileAccessSuite struct {
	BaseSnapSuite

	fakeHome string
}

var _ = Suite(&SnapRoutineFileAccessSuite{})

func (s *SnapRoutineFileAccessSuite) SetUpTest(c *C) {
	s.BaseSnapSuite.SetUpTest(c)

	s.fakeHome = c.MkDir()
	u := mylog.Check2(user.Current())

	s.AddCleanup(snap.MockUserCurrent(func() (*user.User, error) {
		return &user.User{Uid: u.Uid, HomeDir: s.fakeHome}, nil
	}))
}

func (s *SnapRoutineFileAccessSuite) setUpClient(c *C, isClassic, hasHome, hasRemovableMedia bool) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snaps/hello":
			c.Check(r.Method, Equals, "GET")
			// snap hello at revision 100
			response := mockInfoJSONNoLicense
			if isClassic {
				response = strings.Replace(response, `"confinement": "strict"`, `"confinement": "classic"`, 1)
			}
			fmt.Fprintln(w, response)
		case "/v2/connections":
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/connections")
			c.Check(r.URL.Query(), DeepEquals, url.Values{
				"snap": []string{"hello"},
			})
			connections := []client.Connection{}
			if hasHome {
				connections = append(connections, client.Connection{
					Slot: client.SlotRef{
						Snap: "core",
						Name: "home",
					},
					Plug: client.PlugRef{
						Snap: "hello",
						Name: "home",
					},
					Interface: "home",
				})
			}
			if hasRemovableMedia {
				connections = append(connections, client.Connection{
					Slot: client.SlotRef{
						Snap: "core",
						Name: "removable-media",
					},
					Plug: client.PlugRef{
						Snap: "hello",
						Name: "removable-media",
					},
					Interface: "removable-media",
				})
			}
			result := client.Connections{Established: connections}
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": result,
			})
		default:
			c.Fatalf("unexpected request: %v", r)
		}
	})
}

func (s *SnapRoutineFileAccessSuite) checkAccess(c *C, path, access string) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"routine", "file-access", "hello", path}))

	c.Check(s.Stdout(), Equals, access)
	c.Check(s.Stderr(), Equals, "")
	s.ResetStdStreams()
}

func (s *SnapRoutineFileAccessSuite) checkBasicAccess(c *C) {
	// Check access to SNAP_DATA and SNAP_COMMON
	s.checkAccess(c, "/var/snap", "hidden\n")
	s.checkAccess(c, "/var/snap/other-snap", "hidden\n")
	s.checkAccess(c, "/var/snap/hello", "read-only\n")
	s.checkAccess(c, "/var/snap/hello/common", "read-write\n")
	s.checkAccess(c, "/var/snap/hello/current", "read-write\n")
	s.checkAccess(c, "/var/snap/hello/100", "read-write\n")
	s.checkAccess(c, "/var/snap/hello/99", "read-only\n")

	// Check access to SNAP_USER_DATA and SNAP_USER_COMMON
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap"), "hidden\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/other-snap"), "hidden\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/hello"), "read-only\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/hello/common"), "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/hello/current"), "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/hello/100"), "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/hello/99"), "read-only\n")
}

func (s *SnapRoutineFileAccessSuite) TestAccessDefault(c *C) {
	s.setUpClient(c, false, false, false)
	s.checkBasicAccess(c)

	// No access to root
	s.checkAccess(c, "/", "hidden\n")
	s.checkAccess(c, "/usr/lib/libfoo.so", "hidden\n")
	// No access to removable media
	s.checkAccess(c, "/media/foo", "hidden\n")
	// No access to home directory
	s.checkAccess(c, s.fakeHome, "hidden\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "Documents"), "hidden\n")
}

func (s *SnapRoutineFileAccessSuite) TestAccessClassicConfinement(c *C) {
	s.setUpClient(c, true, false, false)

	// Classic confinement snaps run in the host file system
	// namespace, so have access to everything.
	s.checkAccess(c, "/", "read-write\n")
	s.checkAccess(c, "/usr/lib/libfoo.so", "read-write\n")
	s.checkAccess(c, "/", "read-write\n")
	s.checkAccess(c, s.fakeHome, "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "snap/other-snap"), "read-write\n")
}

func (s *SnapRoutineFileAccessSuite) TestAccessHomeInterface(c *C) {
	s.setUpClient(c, false, true, false)
	s.checkBasicAccess(c)

	// Access to non-hidden files in the home directory
	s.checkAccess(c, s.fakeHome, "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "Documents/foo.txt"), "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, "Documents/.hidden"), "read-write\n")
	s.checkAccess(c, filepath.Join(s.fakeHome, ".config"), "hidden\n")
}

func (s *SnapRoutineFileAccessSuite) TestAccessRemovableMedia(c *C) {
	s.setUpClient(c, false, false, true)
	s.checkBasicAccess(c)

	s.checkAccess(c, "/mnt", "read-write\n")
	s.checkAccess(c, "/mnt/path/file.txt", "read-write\n")
	s.checkAccess(c, "/media", "read-write\n")
	s.checkAccess(c, "/media/path/file.txt", "read-write\n")
	s.checkAccess(c, "/run/media", "read-write\n")
	s.checkAccess(c, "/run/media/path/file.txt", "read-write\n")
}
