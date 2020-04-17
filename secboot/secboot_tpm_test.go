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
	"os"
	"testing"

	"github.com/chrisccoulson/go-tpm2"
	sb "github.com/snapcore/secboot"

	. "gopkg.in/check.v1"

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
