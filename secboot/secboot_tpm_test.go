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
	sbEnabled := []uint8{6, 0, 0, 0, 1}
	sbDisabled := []uint8{6, 0, 0, 0, 0}

	type testCase struct {
		hasTPM bool
		sbData []uint8
		errStr string
	}
	for _, t := range []testCase{
		{true, sbEnabled, ""},
		{true, sbEmpty, "cannot read secure boot file: EOF"},
		{false, sbEmpty, "cannot read secure boot file: EOF"},
		{true, sbDisabled, "secure boot is disabled"},
		{false, sbEnabled, "cannot connect to TPM device: TPM not available"},
		{false, sbDisabled, "secure boot is disabled"},
	} {
		restore := secboot.MockSecbootConnectToDefaultTPM(func() (*sb.TPMConnection, error) {
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

		err := secboot.CreateEfivarsSecureBoot(t.sbData)
		c.Assert(err, IsNil)

		err = secboot.CheckKeySealingSupported()
		if t.errStr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, t.errStr)
		}
	}
}
