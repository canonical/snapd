// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type authRequestorSuite struct {
	testutil.BaseTest

	inputFile             string
	errorFile             string
	systemctlVersionFile  string
	systemdAskPasswordCmd *testutil.MockCmd
}

var _ = Suite(&authRequestorSuite{})

func (s *authRequestorSuite) setInput(c *C, input string) {
	err := os.WriteFile(s.inputFile, []byte(input), 0644)
	c.Assert(err, IsNil)
}

func (s *authRequestorSuite) setError(c *C, message string) {
	err := os.WriteFile(s.errorFile, []byte(message), 0644)
	c.Assert(err, IsNil)
}

func (s *authRequestorSuite) setSystemdVersion(c *C, version string) {
	fullVersion := fmt.Sprintf("systemd %[1]s (%[1]s.4-1ubuntu3)\n+PAM +AUDIT...\n", version)
	err := os.WriteFile(s.systemctlVersionFile, []byte(fullVersion), 0644)
	c.Assert(err, IsNil)
}

func (s *authRequestorSuite) SetUpTest(c *C) {
	commandInputs := c.MkDir()
	s.inputFile = filepath.Join(commandInputs, "input")
	s.errorFile = filepath.Join(commandInputs, "error")
	err := os.WriteFile(s.errorFile, []byte("unset"), 0644)
	c.Assert(err, IsNil)
	script := fmt.Sprintf(`if [ -r '%[1]s' ]; then cat '%[1]s'; else cat '%[2]s'; exit 1; fi`, s.inputFile, s.errorFile)
	fmt.Printf("%s\n", script)
	s.systemdAskPasswordCmd = testutil.MockCommand(c, "systemd-ask-password", script)
	s.AddCleanup(s.systemdAskPasswordCmd.Restore)
	systemctlResults := c.MkDir()
	s.systemctlVersionFile = filepath.Join(systemctlResults, "version")
	systemctl := fmt.Sprintf(`if [ "${1-}" = --version ]; then cat %[1]s; else exit 1; fi`, s.systemctlVersionFile)
	systemctlCmd := testutil.MockCommand(c, "systemctl", systemctl)
	s.AddCleanup(systemctlCmd.Restore)
}

func (s *authRequestorSuite) TestRequestPassphrase(c *C) {
	authRequestor := secboot.NewSystemdAuthRequestor()
	s.setInput(c, "thepassphrase\n")
	s.setSystemdVersion(c, "256")

	input, err := authRequestor.RequestPassphrase("some-volume", "some-device")
	c.Assert(err, IsNil)
	c.Check(input, Equals, "thepassphrase")

	c.Check(s.systemdAskPasswordCmd.Calls(), DeepEquals, [][]string{
		{"systemd-ask-password", "--icon", "drive-harddisk", "--id", "secboot.test:some-device", "--credential=snapd.passphrase", "Please enter the passphrase for volume some-volume for device some-device"},
	})
}

func (s *authRequestorSuite) TestRequestPassphraseOldSystemd(c *C) {
	authRequestor := secboot.NewSystemdAuthRequestor()
	s.setInput(c, "thepassphrase\n")
	s.setSystemdVersion(c, "248")

	input, err := authRequestor.RequestPassphrase("some-volume", "some-device")
	c.Assert(err, IsNil)
	c.Check(input, Equals, "thepassphrase")

	c.Check(s.systemdAskPasswordCmd.Calls(), DeepEquals, [][]string{
		{"systemd-ask-password", "--icon", "drive-harddisk", "--id", "secboot.test:some-device", "Please enter the passphrase for volume some-volume for device some-device"},
	})
}

func (s *authRequestorSuite) TestRequestPassphraseFailure(c *C) {
	authRequestor := secboot.NewSystemdAuthRequestor()
	s.setError(c, "blah blah\n")
	s.setSystemdVersion(c, "256")

	_, err := authRequestor.RequestPassphrase("some-volume", "some-device")
	c.Assert(err, ErrorMatches, "cannot execute systemd-ask-password: exit status 1")

	c.Check(s.systemdAskPasswordCmd.Calls(), DeepEquals, [][]string{
		{"systemd-ask-password", "--icon", "drive-harddisk", "--id", "secboot.test:some-device", "--credential=snapd.passphrase", "Please enter the passphrase for volume some-volume for device some-device"},
	})
}

func (s *authRequestorSuite) TestRequestPassphraseMissingNewLine(c *C) {
	authRequestor := secboot.NewSystemdAuthRequestor()
	s.setInput(c, "blah blah")
	s.setSystemdVersion(c, "256")

	_, err := authRequestor.RequestPassphrase("some-volume", "some-device")
	c.Assert(err, ErrorMatches, "systemd-ask-password output is missing terminating newline")

	c.Check(s.systemdAskPasswordCmd.Calls(), DeepEquals, [][]string{
		{"systemd-ask-password", "--icon", "drive-harddisk", "--id", "secboot.test:some-device", "--credential=snapd.passphrase", "Please enter the passphrase for volume some-volume for device some-device"},
	})
}

func (s *authRequestorSuite) TestRequestRecoveryKey(c *C) {
	authRequestor := secboot.NewSystemdAuthRequestor()
	s.setInput(c, "00001-00002-00003-00004-00005-00006-00007-00008\n")
	s.setSystemdVersion(c, "256")

	input, err := authRequestor.RequestRecoveryKey("some-volume", "some-device")
	c.Assert(err, IsNil)
	var expected sb.RecoveryKey
	copy(expected[:], []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 7, 0, 8, 0})
	c.Check(input, DeepEquals, expected)

	c.Check(s.systemdAskPasswordCmd.Calls(), DeepEquals, [][]string{
		{"systemd-ask-password", "--icon", "drive-harddisk", "--id", "secboot.test:some-device", "--credential=snapd.recovery", "Please enter the recovery key for volume some-volume for device some-device"},
	})
}

func (s *authRequestorSuite) TestRequestRecoveryKeyParseFailure(c *C) {
	authRequestor := secboot.NewSystemdAuthRequestor()
	s.setInput(c, "broken\n")
	s.setSystemdVersion(c, "256")

	_, err := authRequestor.RequestRecoveryKey("some-volume", "some-device")
	c.Assert(err, ErrorMatches, `cannot parse recovery key: .*`)

	c.Check(s.systemdAskPasswordCmd.Calls(), DeepEquals, [][]string{
		{"systemd-ask-password", "--icon", "drive-harddisk", "--id", "secboot.test:some-device", "--credential=snapd.recovery", "Please enter the recovery key for volume some-volume for device some-device"},
	})
}
