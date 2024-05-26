// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package devicestate_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap/snaptest"
)

var _ = Suite(&deviceMgrRecoveryKeysSuite{})

type deviceMgrRecoveryKeysSuite struct {
	deviceMgrBaseSuite
}

func (s *deviceMgrRecoveryKeysSuite) SetUpTest(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}
	s.deviceMgrBaseSuite.setupBaseTest(c, false)
	s.setUC20PCModelInState(c)

	devicestate.SetSystemMode(s.mgr, "run")
}

func mockSnapFDEFile(c *C, fname string, data []byte) {
	p := filepath.Join(dirs.SnapFDEDir, fname)
	mylog.Check(os.MkdirAll(filepath.Dir(p), 0755))

	mylog.Check(os.WriteFile(p, data, 0644))

}

func mockSystemRecoveryKeys(c *C, alsoReinstall bool) {
	// same inputs/outputs as secboot:crypt_test.go in this test
	rkeystr := mylog.Check2(hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e"))

	mockSnapFDEFile(c, "recovery.key", []byte(rkeystr))

	if alsoReinstall {
		skeystr := "1234567890123456"
		mockSnapFDEFile(c, "reinstall.key", []byte(skeystr))
	}
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeysBackwardCompat(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSystemRecoveryKeys(c, true)

	keys := mylog.Check2(s.mgr.EnsureRecoveryKeys())


	c.Assert(keys, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey:  "61665-00531-54469-09783-47273-19035-40077-28287",
		ReinstallKey: "12849-13363-13877-14391-12345-12849-13363-13877",
	})
}

func (s *deviceMgrBaseSuite) setClassicWithModesModelInState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.makeModelAssertionInState(c, "canonical", "pc-22", map[string]interface{}{
		"architecture": "amd64",
		"grade":        "dangerous",
		"base":         "core22",
		"classic":      "true",
		"distribution": "ubuntu",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "22",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "22",
			},
		},
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-22",
		Serial: "serialserialserial",
	})
}

func (s *deviceMgrRecoveryKeysSuite) testEnsureRecoveryKey(c *C, classic bool) {
	if classic {
		s.setClassicWithModesModelInState(c)
	}
	s.state.Lock()
	defer s.state.Unlock()

	_ := mylog.Check2(s.mgr.EnsureRecoveryKeys())
	c.Check(err, ErrorMatches, `system does not use disk encryption`)

	rkeystr := mylog.Check2(hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e"))

	defer devicestate.MockSecbootEnsureRecoveryKey(func(keyFile string, rkeyDevs []secboot.RecoveryKeyDevice) (keys.RecoveryKey, error) {
		c.Check(keyFile, Equals, filepath.Join(dirs.SnapFDEDir, "recovery.key"))
		keyFilePath := "var/lib/snapd/device/fde/ubuntu-save.key"
		if !classic {
			keyFilePath = filepath.Join("system-data", keyFilePath)
		}
		c.Check(rkeyDevs, DeepEquals, []secboot.RecoveryKeyDevice{
			{Mountpoint: boot.InitramfsDataDir},
			{
				Mountpoint:         boot.InitramfsUbuntuSaveDir,
				AuthorizingKeyFile: filepath.Join(boot.InitramfsDataDir, keyFilePath),
			},
		})

		var rkey keys.RecoveryKey
		copy(rkey[:], []byte(rkeystr))
		return rkey, nil
	})()
	mockSnapFDEFile(c, "marker", nil)

	keys := mylog.Check2(s.mgr.EnsureRecoveryKeys())


	c.Assert(keys, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey: "61665-00531-54469-09783-47273-19035-40077-28287",
	})
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKey(c *C) {
	classic := false
	s.testEnsureRecoveryKey(c, classic)
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeyOnClassic(c *C) {
	classic := true
	s.testEnsureRecoveryKey(c, classic)
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeyInstallMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	devicestate.SetSystemMode(s.mgr, "install")

	rkeystr := mylog.Check2(hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e"))

	defer devicestate.MockSecbootEnsureRecoveryKey(func(keyFile string, rkeyDevs []secboot.RecoveryKeyDevice) (keys.RecoveryKey, error) {
		c.Check(keyFile, Equals, filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "recovery.key"))
		c.Check(rkeyDevs, DeepEquals, []secboot.RecoveryKeyDevice{
			{
				Mountpoint: filepath.Dir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")),
			},
			{
				Mountpoint:         boot.InitramfsUbuntuSaveDir,
				AuthorizingKeyFile: filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "ubuntu-save.key"),
			},
		})

		var rkey keys.RecoveryKey
		copy(rkey[:], []byte(rkeystr))
		return rkey, nil
	})()

	p := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "marker")
	mylog.Check(os.MkdirAll(filepath.Dir(p), 0755))

	mylog.Check(os.WriteFile(p, nil, 0644))


	keys := mylog.Check2(s.mgr.EnsureRecoveryKeys())


	c.Assert(keys, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey: "61665-00531-54469-09783-47273-19035-40077-28287",
	})
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeyRecoverMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	devicestate.SetSystemMode(s.mgr, "recover")

	_ := mylog.Check2(s.mgr.EnsureRecoveryKeys())
	c.Check(err, ErrorMatches, `cannot ensure recovery keys from system mode "recover"`)
}

func (s *deviceMgrRecoveryKeysSuite) testRemoveRecoveryKeys(c *C, classic bool) {
	if classic {
		s.setClassicWithModesModelInState(c)
	}
	s.state.Lock()
	defer s.state.Unlock()
	mylog.Check(s.mgr.RemoveRecoveryKeys())
	c.Check(err, ErrorMatches, `system does not use disk encryption`)

	called := false
	rkey := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	defer devicestate.MockSecbootRemoveRecoveryKeys(func(r2k map[secboot.RecoveryKeyDevice]string) error {
		called = true
		keyFilePath := "var/lib/snapd/device/fde/ubuntu-save.key"
		if !classic {
			keyFilePath = filepath.Join("system-data", keyFilePath)
		}
		c.Check(r2k, DeepEquals, map[secboot.RecoveryKeyDevice]string{
			{Mountpoint: boot.InitramfsDataDir}: rkey,
			{
				Mountpoint:         boot.InitramfsUbuntuSaveDir,
				AuthorizingKeyFile: filepath.Join(boot.InitramfsDataDir, keyFilePath),
			}: rkey,
		})
		return nil
	})()
	mockSnapFDEFile(c, "marker", nil)
	mylog.Check(s.mgr.RemoveRecoveryKeys())

	c.Check(called, Equals, true)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeys(c *C) {
	classic := false
	s.testRemoveRecoveryKeys(c, classic)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeysOnClassic(c *C) {
	classic := true
	s.testRemoveRecoveryKeys(c, classic)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeysBackwardCompat(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	called := false
	rkey := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	defer devicestate.MockSecbootRemoveRecoveryKeys(func(r2k map[secboot.RecoveryKeyDevice]string) error {
		called = true
		c.Check(r2k, DeepEquals, map[secboot.RecoveryKeyDevice]string{
			{Mountpoint: boot.InitramfsDataDir}: rkey,
			{
				Mountpoint:         boot.InitramfsUbuntuSaveDir,
				AuthorizingKeyFile: filepath.Join(boot.InitramfsDataDir, "system-data/var/lib/snapd/device/fde/ubuntu-save.key"),
			}: filepath.Join(dirs.SnapFDEDir, "reinstall.key"),
		})
		return nil
	})()
	mockSnapFDEFile(c, "marker", nil)
	mockSnapFDEFile(c, "reinstall.key", nil)
	mylog.Check(s.mgr.RemoveRecoveryKeys())

	c.Check(called, Equals, true)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeysOtherModes(c *C) {
	for _, mode := range []string{"recover", "install"} {
		devicestate.SetSystemMode(s.mgr, mode)
		mylog.Check(s.mgr.RemoveRecoveryKeys())
		c.Check(err, ErrorMatches, fmt.Sprintf(`cannot remove recovery keys from system mode %q`, mode))
	}
}
