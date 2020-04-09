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
	"io/ioutil"
	"os"
	"testing"

	"github.com/chrisccoulson/go-tpm2"
	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
)

func TestSecboot(t *testing.T) { TestingT(t) }

type secbootSuite struct {
}

var _ = Suite(&secbootSuite{})

func (s *secbootSuite) TestCheckKeySealingSupported(c *C) {
	sbEnabled := []uint8{6, 0, 0, 0, 1}
	sbDisabled := []uint8{6, 0, 0, 0, 0}

	tc := func(hasTPM bool, sbData []uint8) error {
		restoreConnectToDefaultTPM := secboot.MockSecbootConnectToDefaultTPM(func() (*sb.TPMConnection, error) {
			if !hasTPM {
				return nil, errors.New("TPM not available")
			}
			tcti, err := os.Open("/dev/null")
			c.Assert(err, IsNil)
			tpm, err := tpm2.NewTPMContext(tcti)
			c.Assert(err, IsNil)
			mockTPM := &sb.TPMConnection{TPMContext: tpm}
			return mockTPM, nil
		})
		defer restoreConnectToDefaultTPM()

		sbFile := "sbfile"
		err := ioutil.WriteFile(sbFile, sbData, 0644)
		c.Assert(err, IsNil)
		defer os.Remove(sbFile)

		restoreSecureBootFile := secboot.MockEfivarsSecureBootFile(sbFile)
		defer restoreSecureBootFile()

		return secboot.CheckKeySealingSupported()
	}

	c.Assert(tc(true, sbEnabled), IsNil)
	c.Assert(tc(true, sbDisabled), ErrorMatches, "secure boot is disabled")
	c.Assert(tc(false, sbEnabled), ErrorMatches, "cannot connect to TPM device: TPM not available")
	c.Assert(tc(false, sbDisabled), ErrorMatches, "secure boot is disabled")
}
