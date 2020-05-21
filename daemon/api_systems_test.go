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

package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func (s *apiSuite) mockSystemSeeds(c *check.C) (restore func()) {
	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: seedtest.SeedSnaps{
			StoreSigning: s.storeSigning,
			Brands:       s.brands,
		},
		SeedDir: dirs.SnapSeedDir,
	}

	restore = seed.MockTrusted(seed20.StoreSigning.Trusted)

	assertstest.AddMany(s.storeSigning.Database, s.brands.AccountsAndKeys("my-brand")...)
	// add essential snaps
	seed20.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "my-brand", s.storeSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc\nversion: 1\ntype: gadget\nbase: core20", nil, snap.R(1), "my-brand", s.storeSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc-kernel\nversion: 1\ntype: kernel", nil, snap.R(1), "my-brand", s.storeSigning.Database)
	seed20.MakeAssertedSnap(c, "name: core20\nversion: 1\ntype: base", nil, snap.R(1), "my-brand", s.storeSigning.Database)
	seed20.MakeSeed(c, "20191119", "my-brand", "my-model", map[string]interface{}{
		"display-name": "my fancy model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)
	seed20.MakeSeed(c, "20200318", "my-brand", "my-model-2", map[string]interface{}{
		"display-name": "same brand different model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)

	return restore
}

func (s *apiSuite) TestSystemsGetSome(c *check.C) {
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(mgr)

	st := d.overlord.State()
	st.Lock()
	st.Set("seeded-systems", []map[string]interface{}{{
		"system": "20200318", "model": "my-model-2", "brand-id": "my-brand",
		"revision": 2, "timestamp": "2009-11-10T23:00:00Z",
		"seed-time": "2009-11-10T23:00:00Z",
	}})
	st.Unlock()

	restore := s.mockSystemSeeds(c)
	defer restore()

	req, err := http.NewRequest("GET", "/v2/systems", nil)
	c.Assert(err, check.IsNil)
	rsp := getSystems(systemsCmd, req, nil).(*resp)

	c.Assert(rsp.Status, check.Equals, 200)
	sys := rsp.Result.(*systemsResponse)

	c.Assert(sys, check.DeepEquals, &systemsResponse{
		Systems: []client.System{
			{
				Current: false,
				Label:   "20191119",
				Model: client.SystemModelData{
					Model:       "my-model",
					BrandID:     "my-brand",
					DisplayName: "my fancy model",
				},
				Brand: snap.StoreAccount{
					ID:          "my-brand",
					Username:    "my-brand",
					DisplayName: "My-brand",
					Validation:  "unproven",
				},
				Actions: []client.SystemAction{
					{Title: "Install", Mode: "install"},
				},
			}, {
				Current: true,
				Label:   "20200318",
				Model: client.SystemModelData{
					Model:       "my-model-2",
					BrandID:     "my-brand",
					DisplayName: "same brand different model",
				},
				Brand: snap.StoreAccount{
					ID:          "my-brand",
					Username:    "my-brand",
					DisplayName: "My-brand",
					Validation:  "unproven",
				},
				Actions: []client.SystemAction{
					{Title: "Reinstall", Mode: "install"},
					{Title: "Recover", Mode: "recover"},
					{Title: "Run normally", Mode: "run"},
				},
			},
		}})
}

func (s *apiSuite) TestSystemsGetNone(c *check.C) {
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, check.IsNil)

	// model assertion setup
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(mgr)

	// no system seeds
	req, err := http.NewRequest("GET", "/v2/systems", nil)
	c.Assert(err, check.IsNil)
	rsp := getSystems(systemsCmd, req, nil).(*resp)

	c.Assert(rsp.Status, check.Equals, 200)
	sys := rsp.Result.(*systemsResponse)

	c.Assert(sys, check.DeepEquals, &systemsResponse{})
}

func (s *apiSuite) TestSystemActionRequestErrors(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(mgr)

	restore := s.mockSystemSeeds(c)
	defer restore()

	type table struct {
		label, body, error string
		status             int
	}
	tests := []table{
		{
			label:  "foobar",
			body:   `"bogus"`,
			error:  "cannot decode request body into system action:.*",
			status: 400,
		}, {
			label:  "",
			body:   `{"action":"do","mode":"install"}`,
			error:  "system action requires the system label to be provided",
			status: 400,
		}, {
			label:  "foobar",
			body:   `{"action":"do"}`,
			error:  "system action requires the mode to be provided",
			status: 400,
		}, {
			label:  "foobar",
			body:   `{"action":"nope","mode":"install"}`,
			error:  `unsupported action "nope"`,
			status: 400,
		}, {
			label:  "foobar",
			body:   `{"action":"do","mode":"install"}`,
			error:  `requested seed system "foobar" does not exist`,
			status: 404,
		}, {
			// valid system label but incorrect action
			label:  "20191119",
			body:   `{"action":"do","mode":"foobar"}`,
			error:  `requested action is not supported by system "20191119"`,
			status: 400,
		},
	}
	for _, tc := range tests {
		s.vars = map[string]string{"label": tc.label}
		c.Logf("tc: %#v", tc)
		req, err := http.NewRequest("POST", "/v2/systems/"+tc.label, strings.NewReader(tc.body))
		c.Assert(err, check.IsNil)
		rsp := postSystemsAction(systemsActionCmd, req, nil).(*resp)
		c.Assert(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, tc.status)
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.error)
	}
}

func (s *apiSuite) TestSystemActionRequestWithSeeded(c *check.C) {
	bt := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bt)
	defer func() { bootloader.Force(nil) }()

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()

	restore := s.mockSystemSeeds(c)
	defer restore()

	model := s.brands.Model("my-brand", "pc", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"grade": "dangerous",
		"base":  "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("oc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	currentSystem := []map[string]interface{}{{
		"system": "20191119", "model": "my-model", "brand-id": "my-brand",
		"revision": 2, "timestamp": "2009-11-10T23:00:00Z",
		"seed-time": "2009-11-10T23:00:00Z",
	}}

	tt := []struct {
		currentMode    string
		actionMode     string
		expUnsupported bool
		expRestart     bool
		comment        string
	}{
		{
			// from run mode -> install mode works to reinstall the system
			currentMode: "run",
			actionMode:  "install",
			expRestart:  true,
			comment:     "run mode to install mode",
		},
		{
			// from run mode -> recover mode works to recover the system
			currentMode: "run",
			actionMode:  "recover",
			expRestart:  true,
			comment:     "run mode to recover mode",
		},
		{
			// from run mode -> run mode is no-op
			currentMode: "run",
			actionMode:  "run",
			comment:     "run mode to run mode",
		},
		{
			// from recover mode -> run mode works to stop recovering and "restore" the system to normal
			currentMode: "recover",
			actionMode:  "run",
			expRestart:  true,
			comment:     "recover mode to run mode",
		},
		{
			// from recover mode -> install mode works to stop recovering and reinstall the system if all is lost
			currentMode: "recover",
			actionMode:  "install",
			expRestart:  true,
			comment:     "recover mode to install mode",
		},
		{
			// from recover mode -> recover mode is no-op
			currentMode:    "recover",
			actionMode:     "recover",
			expUnsupported: true,
			comment:        "recover mode to recover mode",
		},
		{
			// from install mode -> install mode is no-no
			currentMode:    "install",
			actionMode:     "install",
			expUnsupported: true,
			comment:        "install mode to install mode not supported",
		},
		{
			// from install mode -> run mode is no-no
			currentMode:    "install",
			actionMode:     "run",
			expUnsupported: true,
			comment:        "install mode to run mode not supported",
		},
		{
			// from install mode -> recover mode is no-no
			currentMode:    "install",
			actionMode:     "recover",
			expUnsupported: true,
			comment:        "install mode to recover mode not supported",
		},
	}
	s.vars = map[string]string{"label": "20191119"}

	for _, tc := range tt {
		c.Logf("tc: %v", tc.comment)
		// daemon setup - need to do this per-test because we need to re-read
		// the modeenv during devicemgr startup
		m := boot.Modeenv{
			Mode: tc.currentMode,
		}
		if tc.currentMode != "run" {
			m.RecoverySystem = "20191119"
		}
		err := m.WriteTo("")
		c.Assert(err, check.IsNil)
		d := s.daemon(c)
		st := d.overlord.State()
		st.Lock()
		// devicemgr needs boot id to request a reboot
		st.VerifyReboot("boot-id-0")
		// device model
		assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
		assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
		s.mockModel(c, st, model)
		if tc.currentMode == "run" {
			// only set in run mode
			st.Set("seeded-systems", currentSystem)
		}
		st.Unlock()

		body := map[string]string{
			"action": "do",
			"mode":   tc.actionMode,
		}
		b, err := json.Marshal(body)
		c.Assert(err, check.IsNil, check.Commentf(tc.comment))
		buf := bytes.NewBuffer(b)
		req, err := http.NewRequest("POST", "/v2/systems/20191119", buf)
		c.Assert(err, check.IsNil, check.Commentf(tc.comment))
		// as root
		req.RemoteAddr = "pid=100;uid=0;socket=;"
		rec := httptest.NewRecorder()
		systemsActionCmd.ServeHTTP(rec, req)
		if tc.expUnsupported {
			c.Check(rec.Code, check.Equals, 400, check.Commentf(tc.comment))
		} else {
			c.Check(rec.Code, check.Equals, 200, check.Commentf(tc.comment))
		}

		var rspBody map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &rspBody)
		c.Assert(err, check.IsNil, check.Commentf(tc.comment))

		var expResp map[string]interface{}
		if tc.expUnsupported {
			expResp = map[string]interface{}{
				"result": map[string]interface{}{
					"message": fmt.Sprintf("requested action is not supported by system %q", "20191119"),
				},
				"status":      "Bad Request",
				"status-code": 400.0,
				"type":        "error",
			}
		} else {
			expResp = map[string]interface{}{
				"result":      nil,
				"status":      "OK",
				"status-code": 200.0,
				"type":        "sync",
			}
			if tc.expRestart {
				expResp["maintenance"] = map[string]interface{}{
					"kind":    "system-restart",
					"message": "system is restarting",
				}

				// daemon is not started, only check whether reboot was scheduled as expected

				// reboot flag
				c.Check(d.restartSystem, check.Equals, state.RestartSystemNow, check.Commentf(tc.comment))
				// slow reboot schedule
				c.Check(cmd.Calls(), check.DeepEquals, [][]string{
					{"shutdown", "-r", "+10", "reboot scheduled to update the system"},
				},
					check.Commentf(tc.comment),
				)
			}
		}

		c.Assert(rspBody, check.DeepEquals, expResp, check.Commentf(tc.comment))

		cmd.ForgetCalls()
		s.d = nil
	}

}

func (s *apiSuite) TestSystemActionBrokenSeed(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(mgr)

	restore := s.mockSystemSeeds(c)
	defer restore()

	err = os.Remove(filepath.Join(dirs.SnapSeedDir, "systems", "20191119", "model"))
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"label": "20191119"}
	body := `{"action":"do","title":"reinstall","mode":"install"}`
	req, err := http.NewRequest("POST", "/v2/systems/20191119", strings.NewReader(body))
	c.Assert(err, check.IsNil)
	rsp := postSystemsAction(systemsActionCmd, req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.ErrorResult().Message, check.Matches, `cannot load seed system: cannot load assertions: .*`)
}

func (s *apiSuite) TestSystemActionNonRoot(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(mgr)

	s.vars = map[string]string{"label": "20191119"}
	body := `{"action":"do","title":"reinstall","mode":"install"}`

	// pretend to be a simple user
	req, err := http.NewRequest("POST", "/v2/systems/20191119", strings.NewReader(body))
	c.Assert(err, check.IsNil)
	// non root
	req.RemoteAddr = "pid=100;uid=1234;socket=;"

	rec := httptest.NewRecorder()
	systemsActionCmd.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 401)

	var rspBody map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &rspBody)
	c.Check(err, check.IsNil)
	c.Check(rspBody, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "access denied",
			"kind":    "login-required",
		},
		"status":      "Unauthorized",
		"status-code": 401.0,
		"type":        "error",
	})
}
