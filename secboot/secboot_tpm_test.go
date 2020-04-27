// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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

package secboot_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/canonical/go-tpm2"
	sb "github.com/snapcore/secboot"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

func TestSecboot(t *testing.T) { TestingT(t) }

type secbootSuite struct {
	testutil.BaseTest
}

var _ = Suite(&secbootSuite{})

func (s *secbootSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *secbootSuite) TestCheckKeySealingSupported(c *C) {
	sbEmpty := []uint8{}
	sbEnabled := []uint8{1}
	sbDisabled := []uint8{0}
	efiNotSupported := []uint8(nil)

	type testCase struct {
		hasTPM bool
		sbData []uint8
		errStr string
	}
	for _, t := range []testCase{
		{true, sbEnabled, ""},
		{true, sbEmpty, "secure boot variable does not exist"},
		{false, sbEmpty, "secure boot variable does not exist"},
		{true, sbDisabled, "secure boot is disabled"},
		{false, sbEnabled, "cannot connect to TPM device: TPM not available"},
		{false, sbDisabled, "secure boot is disabled"},
		{true, efiNotSupported, "not a supported EFI system"},
	} {
		c.Logf("t: %v %v %q", t.hasTPM, t.sbData, t.errStr)

		restore := secboot.MockSbConnectToDefaultTPM(func() (*sb.TPMConnection, error) {
			if !t.hasTPM {
				return nil, errors.New("TPM not available")
			}
			tcti, err := os.Open("/dev/null")
			c.Assert(err, IsNil)
			tpm, err := tpm2.NewTPMContext(tcti)
			c.Assert(err, IsNil)
			mockTPM := &sb.TPMConnection{TPMContext: tpm}
			return mockTPM, nil
		})
		defer restore()

		var vars map[string][]byte
		if t.sbData != nil {
			vars = map[string][]byte{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c": t.sbData}
		}
		restoreEfiVars := efi.MockVars(vars, nil)
		defer restoreEfiVars()

		err := secboot.CheckKeySealingSupported()
		if t.errStr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, t.errStr)
		}
	}
}

func (s *secbootSuite) TestUnlockEncryptedPartition(c *C) {
	for _, tc := range []struct {
		lock     bool
		activate bool
		actErr   error
		err      string
	}{
		{lock: true, activate: true, actErr: nil, err: ""},
		{lock: true, activate: true, actErr: errors.New("didn't activate"), err: ""},
		{lock: true, activate: false, actErr: errors.New("didn't activate"), err: `cannot activate encrypted device "device": didn't activate`},
		{lock: false, activate: true, actErr: nil, err: ""},
	} {
		calls := 0
		restore := secboot.MockSbActivateVolumeWithTPMSealedKey(func(tpm *sb.TPMConnection, volumeName, sourceDevicePath,
			keyPath string, pinReader io.Reader, options *sb.ActivateWithTPMSealedKeyOptions) (bool, error) {
			calls++
			c.Assert(volumeName, Equals, "name")
			c.Assert(sourceDevicePath, Equals, "device")
			c.Assert(keyPath, Equals, "keyfile")
			c.Assert(pinReader, Equals, nil)
			c.Assert(options.LockSealedKeyAccess, Equals, tc.lock)
			return tc.activate, tc.actErr

		})
		defer restore()

		t := secboot.NewTPMFromConnection(nil)
		err := secboot.UnlockEncryptedPartition(t, "name", "device", "keyfile", "pinfile", tc.lock)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
		c.Assert(calls, Equals, 1)
	}
}

func (s *secbootSuite) TestMeasureEpoch(c *C) {
	pcr := 0
	calls := 0
	restore := secboot.MockSbMeasureSnapSystemEpochToTPM(func(tpm *sb.TPMConnection, pcrIndex int) error {
		calls++
		pcr = pcrIndex
		return nil
	})
	defer restore()

	t := secboot.NewTPMFromConnection(nil)
	err := secboot.MeasureEpoch(t)
	c.Assert(err, IsNil)
	c.Assert(calls, Equals, 1)
	c.Assert(pcr, Equals, 12)
}

func (s *secbootSuite) TestMeasureModel(c *C) {
	pcr := 0
	calls := 0
	var model *asserts.Model
	restore := secboot.MockSbMeasureSnapModelToTPM(func(tpm *sb.TPMConnection, pcrIndex int, m *asserts.Model) error {
		calls++
		pcr = pcrIndex
		model = m
		return nil
	})
	defer restore()

	t := secboot.NewTPMFromConnection(nil)
	myModel := &asserts.Model{}
	err := secboot.MeasureModel(t, myModel)
	c.Assert(err, IsNil)
	c.Assert(calls, Equals, 1)
	c.Assert(pcr, Equals, 12)
	c.Assert(model, Equals, myModel)
}
