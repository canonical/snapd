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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/secboot/keys"
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

	devicestate.SetSystemMode(s.mgr, "run")
}

func mockSnapFDEFile(c *C, fname string, data []byte) {
	p := filepath.Join(dirs.SnapFDEDir, fname)
	err := os.MkdirAll(filepath.Dir(p), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(p, data, 0644)
	c.Assert(err, IsNil)
}

func mockSystemRecoveryKeys(c *C, alsoReinstall bool) {
	// same inputs/outputs as secboot:crypt_test.go in this test
	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	mockSnapFDEFile(c, "recovery.key", []byte(rkeystr))

	if alsoReinstall {
		skeystr := "1234567890123456"
		mockSnapFDEFile(c, "reinstall.key", []byte(skeystr))
	}
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeysBackwardCompat(c *C) {
	mockSystemRecoveryKeys(c, true)

	keys, err := s.mgr.EnsureRecoveryKeys()
	c.Assert(err, IsNil)

	c.Assert(keys, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey:  "61665-00531-54469-09783-47273-19035-40077-28287",
		ReinstallKey: "12849-13363-13877-14391-12345-12849-13363-13877",
	})
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKey(c *C) {
	_, err := s.mgr.EnsureRecoveryKeys()
	c.Check(err, ErrorMatches, `system does not use disk encryption`)

	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	defer devicestate.MockSecbootEnsureRecoveryKey(func(keyFile string, mountPoints []string) (keys.RecoveryKey, error) {
		c.Check(keyFile, Equals, filepath.Join(dirs.SnapFDEDir, "recovery.key"))
		c.Check(mountPoints, DeepEquals, []string{boot.InitramfsDataDir, boot.InitramfsUbuntuSaveDir})

		var rkey keys.RecoveryKey
		copy(rkey[:], []byte(rkeystr))
		return rkey, nil
	})()
	mockSnapFDEFile(c, "marker", nil)

	keys, err := s.mgr.EnsureRecoveryKeys()
	c.Assert(err, IsNil)

	c.Assert(keys, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey: "61665-00531-54469-09783-47273-19035-40077-28287",
	})
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeyInstallMode(c *C) {
	devicestate.SetSystemMode(s.mgr, "install")

	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	defer devicestate.MockSecbootEnsureRecoveryKey(func(keyFile string, mountPoints []string) (keys.RecoveryKey, error) {
		c.Check(keyFile, Equals, filepath.Join(boot.InstallHostFDEDataDir, "recovery.key"))
		c.Check(mountPoints, DeepEquals, []string{filepath.Dir(boot.InstallHostWritableDir), boot.InitramfsUbuntuSaveDir})

		var rkey keys.RecoveryKey
		copy(rkey[:], []byte(rkeystr))
		return rkey, nil
	})()

	p := filepath.Join(boot.InstallHostFDEDataDir, "marker")
	err = os.MkdirAll(filepath.Dir(p), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(p, nil, 0644)
	c.Assert(err, IsNil)

	keys, err := s.mgr.EnsureRecoveryKeys()
	c.Assert(err, IsNil)

	c.Assert(keys, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey: "61665-00531-54469-09783-47273-19035-40077-28287",
	})
}

func (s *deviceMgrRecoveryKeysSuite) TestEnsureRecoveryKeyRecoverMode(c *C) {
	devicestate.SetSystemMode(s.mgr, "recover")

	_, err := s.mgr.EnsureRecoveryKeys()
	c.Check(err, ErrorMatches, `cannot ensure recovery keys from system mode "recover"`)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeys(c *C) {
	err := s.mgr.RemoveRecoveryKeys()
	c.Check(err, ErrorMatches, `system does not use disk encryption`)

	called := false
	rkey := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	defer devicestate.MockSecbootRemoveRecoveryKeys(func(m2k map[string]string) error {
		called = true
		c.Check(m2k, DeepEquals, map[string]string{
			boot.InitramfsDataDir:       rkey,
			boot.InitramfsUbuntuSaveDir: rkey,
		})
		return nil
	})()
	mockSnapFDEFile(c, "marker", nil)

	err = s.mgr.RemoveRecoveryKeys()
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeysBackwardCompat(c *C) {
	called := false
	rkey := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	defer devicestate.MockSecbootRemoveRecoveryKeys(func(m2k map[string]string) error {
		called = true
		c.Check(m2k, DeepEquals, map[string]string{
			boot.InitramfsDataDir:       rkey,
			boot.InitramfsUbuntuSaveDir: filepath.Join(dirs.SnapFDEDir, "reinstall.key"),
		})
		return nil
	})()
	mockSnapFDEFile(c, "marker", nil)
	mockSnapFDEFile(c, "reinstall.key", nil)

	err := s.mgr.RemoveRecoveryKeys()
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
}

func (s *deviceMgrRecoveryKeysSuite) TestRemoveRecoveryKeysOtherModes(c *C) {
	for _, mode := range []string{"recover", "install"} {
		devicestate.SetSystemMode(s.mgr, mode)

		err := s.mgr.RemoveRecoveryKeys()
		c.Check(err, ErrorMatches, fmt.Sprintf(`cannot remove recovery keys from system mode %q`, mode))
	}
}
