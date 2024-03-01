// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2021 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

var _ = check.Suite(&systemsSuite{})

type systemsSuite struct {
	apiBaseSuite

	seedModelForLabel20191119 *asserts.Model
}

func (s *systemsSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectRootAccess()
}

var pcGadgetUCYaml = `
volumes:
  pc:
    bootloader: grub
    schema: gpt
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-boot
        filesystem: ext4
        size: 750M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-save
        size: 16M
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-data
        filesystem: ext4
        size: 1G
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`

func (s *systemsSuite) mockSystemSeeds(c *check.C) (restore func()) {
	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: seedtest.SeedSnaps{
			StoreSigning: s.StoreSigning,
			Brands:       s.Brands,
		},
		SeedDir: dirs.SnapSeedDir,
	}

	restore = seed.MockTrusted(seed20.StoreSigning.Trusted)

	assertstest.AddMany(s.StoreSigning.Database, s.Brands.AccountsAndKeys("my-brand")...)
	// add essential snaps
	seed20.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "my-brand", s.StoreSigning.Database)
	gadgetFiles := [][]string{
		{"meta/gadget.yaml", string(pcGadgetUCYaml)},
		{"pc-boot.img", "pc-boot.img content"},
		{"pc-core.img", "pc-core.img content"},
		{"grubx64.efi", "grubx64.efi content"},
		{"shim.efi.signed", "shim.efi.signed content"},
	}
	seed20.MakeAssertedSnap(c, "name: pc\nversion: 1\ntype: gadget\nbase: core20", gadgetFiles, snap.R(1), "my-brand", s.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc-kernel\nversion: 1\ntype: kernel", nil, snap.R(1), "my-brand", s.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: core20\nversion: 1\ntype: base", nil, snap.R(1), "my-brand", s.StoreSigning.Database)
	s.seedModelForLabel20191119 = seed20.MakeSeed(c, "20191119", "my-brand", "my-model", map[string]interface{}{
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

func (s *systemsSuite) TestSystemsGetSome(c *check.C) {
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(mgr)

	st := d.Overlord().State()
	st.Lock()
	st.Set("seeded-systems", []map[string]interface{}{{
		"system": "20200318", "model": "my-model-2", "brand-id": "my-brand",
		"revision": 2, "timestamp": "2009-11-10T23:00:00Z",
		"seed-time": "2009-11-10T23:00:00Z",
	}})
	st.Set("default-recovery-system", devicestate.DefaultRecoverySystem{
		System:   "20200318",
		Model:    "my-model-2",
		BrandID:  "my-brand",
		Revision: 2,
	})
	st.Unlock()

	s.expectAuthenticatedAccess()

	restore := s.mockSystemSeeds(c)
	defer restore()

	req, err := http.NewRequest("GET", "/v2/systems", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)

	c.Assert(rsp.Status, check.Equals, 200)
	sys := rsp.Result.(*daemon.SystemsResponse)

	c.Assert(sys, check.DeepEquals, &daemon.SystemsResponse{
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
					{Title: "Recover", Mode: "recover"},
					{Title: "Factory reset", Mode: "factory-reset"},
				},
			}, {
				Current:               true,
				DefaultRecoverySystem: true,
				Label:                 "20200318",
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
					{Title: "Factory reset", Mode: "factory-reset"},
					{Title: "Run normally", Mode: "run"},
				},
			},
		}})
}

func (s *systemsSuite) TestSystemsGetNone(c *check.C) {
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, check.IsNil)

	// model assertion setup
	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(mgr)

	s.expectAuthenticatedAccess()

	// no system seeds
	req, err := http.NewRequest("GET", "/v2/systems", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)

	c.Assert(rsp.Status, check.Equals, 200)
	sys := rsp.Result.(*daemon.SystemsResponse)

	c.Assert(sys, check.DeepEquals, &daemon.SystemsResponse{})
}

func (s *systemsSuite) TestSystemActionRequestErrors(c *check.C) {
	// modenev must be mocked before daemon is initialized
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMockAndStore()

	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(mgr)

	restore := s.mockSystemSeeds(c)
	defer restore()

	st := d.Overlord().State()

	type table struct {
		label, body, error string
		status             int
		unseeded           bool
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
		}, {
			// valid label and action, but seeding is not complete yet
			label:    "20191119",
			body:     `{"action":"do","mode":"install"}`,
			error:    `cannot request system action, system is seeding`,
			status:   500,
			unseeded: true,
		},
	}
	for _, tc := range tests {
		st.Lock()
		if tc.unseeded {
			st.Set("seeded", nil)
			m := boot.Modeenv{
				Mode:           "run",
				RecoverySystem: tc.label,
			}
			err := m.WriteTo("")
			c.Assert(err, check.IsNil)
		} else {
			st.Set("seeded", true)
		}
		st.Unlock()
		c.Logf("tc: %#v", tc)
		req, err := http.NewRequest("POST", path.Join("/v2/systems", tc.label), strings.NewReader(tc.body))
		c.Assert(err, check.IsNil)
		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, tc.status)
		c.Check(rspe.Message, check.Matches, tc.error)
	}
}

func (s *systemsSuite) TestSystemActionRequestWithSeeded(c *check.C) {
	bt := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bt)
	defer func() { bootloader.Force(nil) }()

	nRebootCall := 0
	rebootCheck := func(ra boot.RebootAction, d time.Duration, ri *boot.RebootInfo) error {
		nRebootCall++
		// slow reboot schedule
		c.Check(ra, check.Equals, boot.RebootReboot)
		c.Check(d, check.Equals, 10*time.Minute)
		c.Check(ri, check.IsNil)
		return nil
	}
	r := daemon.MockReboot(rebootCheck)
	defer r()

	restore := s.mockSystemSeeds(c)
	defer restore()

	model := s.Brands.Model("my-brand", "pc", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"grade": "dangerous",
		"base":  "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
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

	numExpRestart := 0
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
			// from run mode -> factory-reset
			currentMode: "run",
			actionMode:  "factory-reset",
			expRestart:  true,
			comment:     "run mode to factory-reset mode",
		},
		{
			// from recover mode -> run mode works to stop
			// recovering and "restore" the system to normal
			currentMode: "recover",
			actionMode:  "run",
			expRestart:  true,
			comment:     "recover mode to run mode",
		},
		{
			// from recover mode -> install mode works to stop
			// recovering and reinstall the system if all is lost
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
			// from recover mode -> factory-reset works
			currentMode: "recover",
			actionMode:  "factory-reset",
			expRestart:  true,
			comment:     "recover mode to factory-reset mode",
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
		st := d.Overlord().State()
		st.Lock()
		// make things look like a reboot
		restart.ReplaceBootID(st, "boot-id-1")
		// device model
		assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
		assertstatetest.AddMany(st, s.Brands.AccountsAndKeys("my-brand")...)
		s.mockModel(st, model)
		if tc.currentMode == "run" {
			// only set in run mode
			st.Set("seeded-systems", currentSystem)
		}
		// the seeding is done
		st.Set("seeded", true)
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
		s.asRootAuth(req)
		rec := httptest.NewRecorder()
		s.serveHTTP(c, rec, req)
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
					"value": map[string]interface{}{
						"op": "reboot",
					},
				}

				// daemon is not started, only check whether reboot was scheduled as expected

				// reboot flag
				numExpRestart++
				c.Check(d.RequestedRestart(), check.Equals, restart.RestartSystemNow, check.Commentf(tc.comment))
			}
		}

		c.Assert(rspBody, check.DeepEquals, expResp, check.Commentf(tc.comment))

		s.resetDaemon()
	}

	// we must have called reboot numExpRestart times
	c.Check(nRebootCall, check.Equals, numExpRestart)
}

func (s *systemsSuite) TestSystemActionBrokenSeed(c *check.C) {
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, check.IsNil)

	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(mgr)

	// the seeding is done
	st := d.Overlord().State()
	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	restore := s.mockSystemSeeds(c)
	defer restore()

	err = os.Remove(filepath.Join(dirs.SnapSeedDir, "systems", "20191119", "model"))
	c.Assert(err, check.IsNil)

	body := `{"action":"do","title":"reinstall","mode":"install"}`
	req, err := http.NewRequest("POST", "/v2/systems/20191119", strings.NewReader(body))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 500)
	c.Check(rspe.Message, check.Matches, `cannot load seed system: cannot load assertions for label "20191119": .*`)
}

func (s *systemsSuite) TestSystemActionNonRoot(c *check.C) {
	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	mgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(mgr)

	body := `{"action":"do","title":"reinstall","mode":"install"}`

	// pretend to be a simple user
	req, err := http.NewRequest("POST", "/v2/systems/20191119", strings.NewReader(body))
	c.Assert(err, check.IsNil)
	// non root
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Assert(rec.Code, check.Equals, 403)

	var rspBody map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &rspBody)
	c.Check(err, check.IsNil)
	c.Check(rspBody, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "access denied",
			"kind":    "login-required",
		},
		"status":      "Forbidden",
		"status-code": 403.0,
		"type":        "error",
	})
}

func (s *systemsSuite) TestSystemRebootNeedsRoot(c *check.C) {
	s.daemon(c)

	restore := daemon.MockDeviceManagerReboot(func(dm *devicestate.DeviceManager, systemLabel, mode string) error {
		c.Fatalf("request reboot should not get called")
		return nil
	})
	defer restore()

	body := `{"action":"reboot"}`
	url := "/v2/systems"
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	c.Assert(err, check.IsNil)
	// non root
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Check(rec.Code, check.Equals, 403)
}

func (s *systemsSuite) TestSystemRebootHappy(c *check.C) {
	s.daemon(c)

	for _, tc := range []struct {
		systemLabel, mode string
	}{
		{"", ""},
		{"20200101", ""},
		{"", "run"},
		{"", "recover"},
		{"", "factory-reset"},
		{"20200101", "run"},
		{"20200101", "recover"},
		{"20200101", "factory-reset"},
	} {
		called := 0
		restore := daemon.MockDeviceManagerReboot(func(dm *devicestate.DeviceManager, systemLabel, mode string) error {
			called++
			c.Check(dm, check.NotNil)
			c.Check(systemLabel, check.Equals, tc.systemLabel)
			c.Check(mode, check.Equals, tc.mode)
			return nil
		})
		defer restore()

		body := fmt.Sprintf(`{"action":"reboot", "mode":"%s"}`, tc.mode)
		url := "/v2/systems"
		if tc.systemLabel != "" {
			url += "/" + tc.systemLabel
		}
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		c.Assert(err, check.IsNil)
		s.asRootAuth(req)

		rec := httptest.NewRecorder()
		s.serveHTTP(c, rec, req)
		c.Check(rec.Code, check.Equals, 200)
		c.Check(called, check.Equals, 1)
	}
}

func (s *systemsSuite) TestSystemRebootUnhappy(c *check.C) {
	s.daemon(c)

	for _, tc := range []struct {
		rebootErr        error
		expectedHttpCode int
		expectedErr      string
	}{
		{fmt.Errorf("boom"), 500, "boom"},
		{os.ErrNotExist, 404, `requested seed system "" does not exist`},
		{devicestate.ErrUnsupportedAction, 400, `requested action is not supported by system ""`},
	} {
		called := 0
		restore := daemon.MockDeviceManagerReboot(func(dm *devicestate.DeviceManager, systemLabel, mode string) error {
			called++
			return tc.rebootErr
		})
		defer restore()

		body := `{"action":"reboot"}`
		url := "/v2/systems"
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		c.Assert(err, check.IsNil)
		s.asRootAuth(req)

		rec := httptest.NewRecorder()
		s.serveHTTP(c, rec, req)
		c.Check(rec.Code, check.Equals, tc.expectedHttpCode)
		c.Check(called, check.Equals, 1)

		var rspBody map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &rspBody)
		c.Check(err, check.IsNil)
		c.Check(rspBody["status-code"], check.Equals, float64(tc.expectedHttpCode))
		result := rspBody["result"].(map[string]interface{})
		c.Check(result["message"], check.Equals, tc.expectedErr)
	}
}

// XXX: duplicated from gadget_test.go
func asOffsetPtr(offs quantity.Offset) *quantity.Offset {
	goff := offs
	return &goff
}

func (s *systemsSuite) TestSystemsGetSystemDetailsForLabel(c *check.C) {
	s.mockSystemSeeds(c)

	s.daemon(c)
	s.expectRootAccess()

	mockGadgetInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"pc": {
				Schema:     "gpt",
				Bootloader: "grub",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName: "foo",
					},
				},
			},
		},
	}

	for _, tc := range []struct {
		disabled, available                bool
		storageSafety                      asserts.StorageSafety
		typ                                secboot.EncryptionType
		unavailableErr, unavailableWarning string

		expectedSupport                                  client.StorageEncryptionSupport
		expectedStorageSafety, expectedUnavailableReason string
	}{
		{
			true, false, asserts.StorageSafetyPreferEncrypted, "", "", "",
			client.StorageEncryptionSupportDisabled, "", "",
		},
		{
			false, false, asserts.StorageSafetyPreferEncrypted, "", "", "unavailable-warn",
			client.StorageEncryptionSupportUnavailable, "prefer-encrypted", "unavailable-warn",
		},
		{
			false, true, asserts.StorageSafetyPreferEncrypted, "cryptsetup", "", "",
			client.StorageEncryptionSupportAvailable, "prefer-encrypted", "",
		},
		{
			false, true, asserts.StorageSafetyPreferUnencrypted, "cryptsetup", "", "",
			client.StorageEncryptionSupportAvailable, "prefer-unencrypted", "",
		},
		{
			false, false, asserts.StorageSafetyEncrypted, "", "unavailable-err", "",
			client.StorageEncryptionSupportDefective, "encrypted", "unavailable-err",
		},
		{
			false, true, asserts.StorageSafetyEncrypted, "", "", "",
			client.StorageEncryptionSupportAvailable, "encrypted", "",
		},
	} {
		mockEncryptionSupportInfo := &install.EncryptionSupportInfo{
			Available:          tc.available,
			Disabled:           tc.disabled,
			StorageSafety:      tc.storageSafety,
			UnavailableErr:     errors.New(tc.unavailableErr),
			UnavailableWarning: tc.unavailableWarning,
		}

		r := daemon.MockDeviceManagerSystemAndGadgetAndEncryptionInfo(func(mgr *devicestate.DeviceManager, label string) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error) {
			c.Check(label, check.Equals, "20191119")
			sys := &devicestate.System{
				Model: s.seedModelForLabel20191119,
				Label: "20191119",
				Brand: s.Brands.Account("my-brand"),
			}
			return sys, mockGadgetInfo, mockEncryptionSupportInfo, nil
		})
		defer r()

		req, err := http.NewRequest("GET", "/v2/systems/20191119", nil)
		c.Assert(err, check.IsNil)
		rsp := s.syncReq(c, req, nil)

		c.Assert(rsp.Status, check.Equals, 200)
		sys := rsp.Result.(client.SystemDetails)
		c.Check(sys, check.DeepEquals, client.SystemDetails{
			Label: "20191119",
			Model: s.seedModelForLabel20191119.Headers(),
			Brand: snap.StoreAccount{
				ID:          "my-brand",
				Username:    "my-brand",
				DisplayName: "My-brand",
				Validation:  "unproven",
			},
			StorageEncryption: &client.StorageEncryption{
				Support:           tc.expectedSupport,
				StorageSafety:     tc.expectedStorageSafety,
				UnavailableReason: tc.expectedUnavailableReason,
			},
			Volumes: mockGadgetInfo.Volumes,
		}, check.Commentf("%v", tc))
	}
}

func (s *systemsSuite) TestSystemsGetSpecificLabelError(c *check.C) {
	s.daemon(c)
	s.expectRootAccess()

	r := daemon.MockDeviceManagerSystemAndGadgetAndEncryptionInfo(func(mgr *devicestate.DeviceManager, label string) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error) {
		return nil, nil, nil, fmt.Errorf("boom")
	})
	defer r()

	req, err := http.NewRequest("GET", "/v2/systems/something", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)

	c.Assert(rspe.Status, check.Equals, 500)
	c.Check(rspe.Message, check.Equals, `boom`)
}

func (s *systemsSuite) TestSystemsGetSpecificLabelNotFoundIntegration(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.daemon(c)
	s.expectRootAccess()

	req, err := http.NewRequest("GET", "/v2/systems/does-not-exist", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 500)
	c.Check(rspe.Message, check.Equals, `cannot load assertions for label "does-not-exist": no seed assertions`)
}

func (s *systemsSuite) TestSystemsGetSpecificLabelIntegration(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	d := s.daemon(c)
	s.expectRootAccess()
	deviceMgr := d.Overlord().DeviceManager()

	restore = s.mockSystemSeeds(c)
	defer restore()

	r := daemon.MockDeviceManagerSystemAndGadgetAndEncryptionInfo(func(mgr *devicestate.DeviceManager, label string) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error) {
		// mockSystemSeed will ensure everything here is coming from
		// the mocked seed except the encryptionInfo
		sys, gadgetInfo, encInfo, err := deviceMgr.SystemAndGadgetAndEncryptionInfo(label)
		// encryptionInfo needs get overridden here to get reliable tests
		encInfo.Available = false
		encInfo.StorageSafety = asserts.StorageSafetyPreferEncrypted
		encInfo.UnavailableWarning = "not encrypting device storage as checking TPM gave: some reason"

		return sys, gadgetInfo, encInfo, err
	})
	defer r()

	req, err := http.NewRequest("GET", "/v2/systems/20191119", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)

	c.Assert(rsp.Status, check.Equals, 200)
	sys := rsp.Result.(client.SystemDetails)

	sd := client.SystemDetails{
		Label: "20191119",
		Model: s.seedModelForLabel20191119.Headers(),
		Actions: []client.SystemAction{
			{Title: "Install", Mode: "install"},
			{Title: "Recover", Mode: "recover"},
			{Title: "Factory reset", Mode: "factory-reset"},
		},

		Brand: snap.StoreAccount{
			ID:          "my-brand",
			Username:    "my-brand",
			DisplayName: "My-brand",
			Validation:  "unproven",
		},
		StorageEncryption: &client.StorageEncryption{
			Support:           "unavailable",
			StorageSafety:     "prefer-encrypted",
			UnavailableReason: "not encrypting device storage as checking TPM gave: some reason",
		},
		Volumes: map[string]*gadget.Volume{
			"pc": {
				Name:       "pc",
				Schema:     "gpt",
				Bootloader: "grub",
				Structure: []gadget.VolumeStructure{
					{
						Name:       "mbr",
						VolumeName: "pc",
						Type:       "mbr",
						Role:       "mbr",
						Offset:     asOffsetPtr(0),
						MinSize:    440,
						Size:       440,
						Content: []gadget.VolumeContent{
							{
								Image: "pc-boot.img",
							},
						},
						YamlIndex: 0,
					},
					{
						Name:       "BIOS Boot",
						VolumeName: "pc",
						Type:       "DA,21686148-6449-6E6F-744E-656564454649",
						MinSize:    1 * quantity.SizeMiB,
						Size:       1 * quantity.SizeMiB,
						Offset:     asOffsetPtr(1 * quantity.OffsetMiB),
						OffsetWrite: &gadget.RelativeOffset{
							RelativeTo: "mbr",
							Offset:     92,
						},
						Content: []gadget.VolumeContent{
							{
								Image: "pc-core.img",
							},
						},
						YamlIndex: 1,
					},
					{
						Name:       "ubuntu-seed",
						Label:      "ubuntu-seed",
						Role:       "system-seed",
						VolumeName: "pc",
						Type:       "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
						Offset:     asOffsetPtr(2 * quantity.OffsetMiB),
						MinSize:    1200 * quantity.SizeMiB,
						Size:       1200 * quantity.SizeMiB,
						Filesystem: "vfat",
						Content: []gadget.VolumeContent{
							{
								UnresolvedSource: "grubx64.efi",
								Target:           "EFI/boot/grubx64.efi",
							},
							{
								UnresolvedSource: "shim.efi.signed",
								Target:           "EFI/boot/bootx64.efi",
							},
						},
						YamlIndex: 2,
					},
					{
						Name:       "ubuntu-boot",
						Label:      "ubuntu-boot",
						Role:       "system-boot",
						VolumeName: "pc",
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Offset:     asOffsetPtr(1202 * quantity.OffsetMiB),
						MinSize:    750 * quantity.SizeMiB,
						Size:       750 * quantity.SizeMiB,
						Filesystem: "ext4",
						YamlIndex:  3,
					},
					{
						Name:       "ubuntu-save",
						Label:      "ubuntu-save",
						Role:       "system-save",
						VolumeName: "pc",
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Offset:     asOffsetPtr(1952 * quantity.OffsetMiB),
						MinSize:    16 * quantity.SizeMiB,
						Size:       16 * quantity.SizeMiB,
						Filesystem: "ext4",
						YamlIndex:  4,
					},
					{
						Name:       "ubuntu-data",
						Label:      "ubuntu-data",
						Role:       "system-data",
						VolumeName: "pc",
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Offset:     asOffsetPtr(1968 * quantity.OffsetMiB),
						MinSize:    1 * quantity.SizeGiB,
						Size:       1 * quantity.SizeGiB,
						Filesystem: "ext4",
						YamlIndex:  5,
					},
				},
			},
		},
	}
	gadget.SetEnclosingVolumeInStructs(sd.Volumes)
	c.Assert(sys, check.DeepEquals, sd)
}

func (s *systemsSuite) TestSystemInstallActionSetupStorageEncryptionCallsDevicestate(c *check.C) {
	s.testSystemInstallActionCallsDevicestate(c, "setup-storage-encryption", daemon.MockDevicestateInstallSetupStorageEncryption)
}

func (s *systemsSuite) TestSystemInstallActionFinishCallsDevicestate(c *check.C) {
	s.testSystemInstallActionCallsDevicestate(c, "finish", daemon.MockDevicestateInstallFinish)
}

func (s *systemsSuite) testSystemInstallActionCallsDevicestate(c *check.C, step string, mocker func(func(st *state.State, label string, onVolumes map[string]*gadget.Volume) (*state.Change, error)) (restore func())) {
	d := s.daemon(c)
	st := d.Overlord().State()

	soon := 0
	_, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
	})
	defer restore()

	nCalls := 0
	var gotOnVolumes map[string]*gadget.Volume
	var gotLabel string
	r := mocker(func(st *state.State, label string, onVolumes map[string]*gadget.Volume) (*state.Change, error) {
		gotLabel = label
		gotOnVolumes = onVolumes
		nCalls++
		return st.NewChange("foo", "..."), nil
	})
	defer r()

	body := map[string]interface{}{
		"action": "install",
		"step":   step,
		"on-volumes": map[string]interface{}{
			"pc": map[string]interface{}{
				"bootloader": "grub",
			},
		},
	}
	b, err := json.Marshal(body)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(b)
	req, err := http.NewRequest("POST", "/v2/systems/20191119", buf)
	c.Assert(err, check.IsNil)

	rsp := s.asyncReq(c, req, nil)

	st.Lock()
	chg := st.Change(rsp.Change)
	st.Unlock()
	c.Check(chg, check.NotNil)
	c.Check(chg.ID(), check.Equals, "1")
	c.Check(nCalls, check.Equals, 1)
	c.Check(gotLabel, check.Equals, "20191119")
	c.Check(gotOnVolumes, check.DeepEquals, map[string]*gadget.Volume{
		"pc": {
			Bootloader: "grub",
		},
	})

	c.Check(soon, check.Equals, 1)
}

func (s *systemsSuite) TestSystemInstallActionGeneratesTasks(c *check.C) {
	d := s.daemon(c)
	st := d.Overlord().State()

	var soon int
	_, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
	})
	defer restore()

	for _, tc := range []struct {
		installStep      string
		expectedNumTasks int
	}{
		{"finish", 1},
		{"setup-storage-encryption", 1},
	} {
		soon = 0
		body := map[string]interface{}{
			"action": "install",
			"step":   tc.installStep,
			"on-volumes": map[string]interface{}{
				"pc": map[string]interface{}{
					"bootloader": "grub",
				},
			},
		}
		b, err := json.Marshal(body)
		c.Assert(err, check.IsNil)
		buf := bytes.NewBuffer(b)
		req, err := http.NewRequest("POST", "/v2/systems/20191119", buf)
		c.Assert(err, check.IsNil)

		rsp := s.asyncReq(c, req, nil)

		st.Lock()
		chg := st.Change(rsp.Change)
		tasks := chg.Tasks()
		st.Unlock()

		c.Check(chg, check.NotNil, check.Commentf("%v", tc))
		c.Check(tasks, check.HasLen, tc.expectedNumTasks, check.Commentf("%v", tc))
		c.Check(soon, check.Equals, 1)
	}
}

func (s *systemsSuite) TestSystemInstallActionErrorMissingVolumes(c *check.C) {
	s.daemon(c)

	for _, tc := range []struct {
		installStep string
		expectedErr string
	}{
		{"finish", `cannot finish install for "20191119": cannot finish install without volumes data (api)`},
		{"setup-storage-encryption", `cannot setup storage encryption for install from "20191119": cannot setup storage encryption without volumes data (api)`},
	} {
		body := map[string]interface{}{
			"action": "install",
			"step":   tc.installStep,
			// note that "on-volumes" is missing which will
			// trigger a bug
		}
		b, err := json.Marshal(body)
		c.Assert(err, check.IsNil)
		buf := bytes.NewBuffer(b)
		req, err := http.NewRequest("POST", "/v2/systems/20191119", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Error(), check.Equals, tc.expectedErr)
	}
}

func (s *systemsSuite) TestSystemInstallActionError(c *check.C) {
	s.daemon(c)

	body := map[string]string{
		"action": "install",
		"step":   "unknown-install-step",
	}
	b, err := json.Marshal(body)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(b)
	req, err := http.NewRequest("POST", "/v2/systems/20191119", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Error(), check.Equals, `unsupported install step "unknown-install-step" (api)`)
}
