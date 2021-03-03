// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package boot_test

import (
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
)

type baseSystemsSuite struct {
	baseBootenvSuite
}

func (s *baseSystemsSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755), IsNil)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)
}

type initramfsMarkTryRecoverySystemSuite struct {
	baseSystemsSuite

	bl *bootloadertest.MockBootloader
}

var _ = Suite(&initramfsMarkTryRecoverySystemSuite{})

func (s *initramfsMarkTryRecoverySystemSuite) SetUpTest(c *C) {
	s.baseSystemsSuite.SetUpTest(c)

	s.bl = bootloadertest.Mock("bootloader", s.bootdir)
	bootloader.Force(s.bl)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *initramfsMarkTryRecoverySystemSuite) testMarkRecoverySystemForRun(c *C, outcome boot.TryRecoverySystemOutcome, expectingStatus string) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(outcome)
	c.Assert(err, IsNil)

	expectedVars := map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "",

		"recovery_system_status": expectingStatus,
		"try_recovery_system":    "1234",
	}

	vars, err := s.bl.GetBootVars("snapd_recovery_mode", "snapd_recovery_system",
		"recovery_system_status", "try_recovery_system")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, expectedVars)

	err = s.bl.SetBootVars(map[string]string{
		// the status is overwritten, even if it's completely bogus
		"recovery_system_status": "foobar",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)

	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(outcome)
	c.Assert(err, IsNil)

	vars, err = s.bl.GetBootVars("snapd_recovery_mode", "snapd_recovery_system",
		"recovery_system_status", "try_recovery_system")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, expectedVars)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkTryRecoverySystemSuccess(c *C) {
	s.testMarkRecoverySystemForRun(c, boot.TryRecoverySystemOutcomeSuccess, "tried")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkRecoverySystemFailure(c *C) {
	s.testMarkRecoverySystemForRun(c, boot.TryRecoverySystemOutcomeFailure, "try")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkRecoverySystemBogus(c *C) {
	s.testMarkRecoverySystemForRun(c, boot.TryRecoverySystemOutcomeInconsistent, "")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkRecoverySystemErr(c *C) {
	s.bl.SetErr = fmt.Errorf("set fails")
	err := boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(boot.TryRecoverySystemOutcomeSuccess)
	c.Assert(err, ErrorMatches, "set fails")
	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(boot.TryRecoverySystemOutcomeFailure)
	c.Assert(err, ErrorMatches, "set fails")
	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(boot.TryRecoverySystemOutcomeInconsistent)
	c.Assert(err, ErrorMatches, "set fails")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemUnset(c *C) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		// system is unset
		"try_recovery_system": "",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `try recovery system is unset but status is "try"`)
	c.Check(boot.IsInconsystemRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemBogus(c *C) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "foobar",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `unexpected recovery system status "foobar"`)
	c.Check(boot.IsInconsystemRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)

	// errors out even if try recovery system label is unset
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "no-label",
		"try_recovery_system":    "",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `unexpected recovery system status "no-label"`)
	c.Check(boot.IsInconsystemRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemNoTryingStatus(c *C) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "",
		"try_recovery_system":    "",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)

	err = s.bl.SetBootVars(map[string]string{
		// status is checked first
		"recovery_system_status": "",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemSameSystem(c *C) {
	// the usual scenario
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, true)

	// pretend the system has already been tried
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, true)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestRecoverySystemSuccessDifferent(c *C) {
	// other system
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "9999",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)

	// same when the other system has already been tried
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "9999",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)
}
