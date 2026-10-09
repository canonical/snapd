// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package apparmor_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/testutil"
)

type networkProbeSuite struct {
	testutil.BaseTest
	fakeroot string
}

var _ = Suite(&networkProbeSuite{})

func (s *networkProbeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.fakeroot = c.MkDir()
	dirs.SetRootDir(s.fakeroot)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.AddCleanup(apparmor.ResetNetworkBugProbe)
}

func (s *networkProbeSuite) writeMarker(c *C, content string) {
	marker := apparmor.NetworkBugMarkerPath()
	c.Assert(os.MkdirAll(filepath.Dir(marker), 0755), IsNil)
	c.Assert(os.WriteFile(marker, []byte(content), 0644), IsNil)
}

// mockParserWithAbi50 makes AppArmorParser() return a mocked parser pinned
// to abi/5.0 via the distro search path.
func (s *networkProbeSuite) mockParserWithAbi50(c *C) *testutil.MockCmd {
	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	s.AddCleanup(mockParserCmd.Restore)
	s.AddCleanup(apparmor.MockParserSearchPath(mockParserCmd.BinDir()))
	s.AddCleanup(apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return false }))

	abi50 := filepath.Join(s.fakeroot, "/etc/apparmor.d/abi/5.0")
	c.Assert(os.MkdirAll(filepath.Dir(abi50), 0755), IsNil)
	c.Assert(os.WriteFile(abi50, nil, 0644), IsNil)
	s.AddCleanup(apparmor.MockHostAbi50File(abi50))
	s.AddCleanup(apparmor.MockHostAbi40File(""))
	s.AddCleanup(apparmor.MockHostAbi30File(""))
	return mockParserCmd
}

func (s *networkProbeSuite) TestMarkerAbsentRunsProbeBuggy(c *C) {
	s.mockParserWithAbi50(c)
	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return true, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 1)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)

	// verdict recorded for other processes
	content, err := os.ReadFile(apparmor.NetworkBugMarkerPath())
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "1\n")
}

func (s *networkProbeSuite) TestMarkerAbsentRunsProbeClean(c *C) {
	s.mockParserWithAbi50(c)
	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return false, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 1)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, false)

	content, err := os.ReadFile(apparmor.NetworkBugMarkerPath())
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "0\n")
}

func (s *networkProbeSuite) TestMarkerPresentSkipsProbe(c *C) {
	s.mockParserWithAbi50(c)
	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return false, nil
	})
	defer restore()

	s.writeMarker(c, "1\n")
	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 0)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)

	apparmor.ResetNetworkBugProbe()
	s.writeMarker(c, "0\n")
	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 0)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, false)
}

func (s *networkProbeSuite) TestProbeOncePerProcess(c *C) {
	s.mockParserWithAbi50(c)
	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return true, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 1)
}

func (s *networkProbeSuite) TestProbeErrorRetriedOnNextCall(c *C) {
	s.mockParserWithAbi50(c)
	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return false, errors.New("probe exploded")
	})
	defer restore()

	// an inconclusive probe is not cached: every caller re-attempts the
	// probe and observes the error
	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), ErrorMatches, "probe exploded")
	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), ErrorMatches, "probe exploded")
	c.Check(ran, Equals, 2)
}

func (s *networkProbeSuite) TestProbeErrorIsInconclusive(c *C) {
	s.mockParserWithAbi50(c)
	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		if ran == 1 {
			return false, errors.New("probe exploded")
		}
		return true, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), ErrorMatches, "probe exploded")
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, false)
	// no verdict recorded, so a later process re-probes
	_, err := os.Stat(apparmor.NetworkBugMarkerPath())
	c.Assert(os.IsNotExist(err), Equals, true)

	// a transient failure is retried by the next call in this process
	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 2)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)
}

func (s *networkProbeSuite) TestNoAbi50SkipsProbe(c *C) {
	// distro parser without abi/5.0: nothing to work around
	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	s.AddCleanup(mockParserCmd.Restore)
	s.AddCleanup(apparmor.MockParserSearchPath(mockParserCmd.BinDir()))
	s.AddCleanup(apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return false }))
	s.AddCleanup(apparmor.MockHostAbi50File(""))
	s.AddCleanup(apparmor.MockHostAbi40File(""))
	s.AddCleanup(apparmor.MockHostAbi30File(""))

	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return false, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 0)
	// nothing recorded so a later snapd refresh introducing abi/5.0 re-probes
	_, err := os.Stat(apparmor.NetworkBugMarkerPath())
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *networkProbeSuite) TestNoParserSkipsProbe(c *C) {
	s.AddCleanup(apparmor.MockParserSearchPath(c.MkDir()))
	s.AddCleanup(apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return false }))

	ran := 0
	restore := apparmor.MockNetworkBugProbeRunner(func(_ *exec.Cmd) (bool, error) {
		ran++
		return false, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(ran, Equals, 0)
}

func (s *networkProbeSuite) TestForcedAbi40ReadsMarkerAcrossProcesses(c *C) {
	// simulate "another process wrote the verdict": no in-process flag,
	// only the marker file
	s.writeMarker(c, "1\n")
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)

	s.writeMarker(c, "0\n")
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, false)
}

// mockProbeStub fakes the "snapd" executable that runNetworkBugProbe re-execs
// as the confined probe stub; the stub script plays the helper's role,
// reporting a failure on stderr and exiting with the given status.
func (s *networkProbeSuite) mockProbeStub(c *C, script string) {
	mockSnapd := testutil.MockCommand(c, "snapd", script)
	s.AddCleanup(mockSnapd.Restore)
	s.AddCleanup(apparmor.MockOsExecutable(func() (string, error) {
		return mockSnapd.Exe(), nil
	}))
}

func (s *networkProbeSuite) TestProbeEaccesOnSocketWriteIsBuggy(c *C) {
	s.mockParserWithAbi50(c)
	s.mockProbeStub(c, "echo 'FAIL write 13' >&2; exit 13")

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)

	content, err := os.ReadFile(apparmor.NetworkBugMarkerPath())
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "1\n")
}

func (s *networkProbeSuite) TestProbeEaccesOnSocketReadIsBuggy(c *C) {
	s.mockParserWithAbi50(c)
	s.mockProbeStub(c, "echo 'FAIL read 13' >&2; exit 13")

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)
}

func (s *networkProbeSuite) TestProbeEaccesOnOtherOperationIsInconclusive(c *C) {
	s.mockParserWithAbi50(c)
	// EACCES on an operation other than the socket write/read is not the
	// bug; it must not downgrade a healthy kernel.
	s.mockProbeStub(c, "echo 'FAIL socket 13' >&2; exit 13")

	err := apparmor.EnsureAppArmor5NetworkBugProbe()
	c.Assert(err, ErrorMatches, `probe failed with exit status 13: FAIL socket 13`)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, false)

	// inconclusive: no verdict recorded, a later process re-probes
	_, err = os.Stat(apparmor.NetworkBugMarkerPath())
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *networkProbeSuite) TestProbeCleanRunIsNotBuggy(c *C) {
	s.mockParserWithAbi50(c)
	s.mockProbeStub(c, "exit 0")

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, false)

	content, err := os.ReadFile(apparmor.NetworkBugMarkerPath())
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "0\n")
}

func (s *networkProbeSuite) TestProbeUsesParserFromEligibilityCheck(c *C) {
	// The parser is captured at the eligibility check, when no marker
	// exists and abi/5.0 is selected. A concurrent verdict from another
	// process landing before the probe compiles its profile must not
	// change the ABI the probe tests.
	mockParserCmd := s.mockParserWithAbi50(c)
	abi50 := filepath.Join(s.fakeroot, "/etc/apparmor.d/abi/5.0")
	abi40 := filepath.Join(s.fakeroot, "/etc/apparmor.d/abi/4.0")
	c.Assert(os.WriteFile(abi40, nil, 0644), IsNil)
	s.AddCleanup(apparmor.MockHostAbi40File(abi40))

	var gotParserArgs []string
	restore := apparmor.MockNetworkBugProbeRunner(func(parser *exec.Cmd) (bool, error) {
		gotParserArgs = append([]string{}, parser.Args...)
		// Simulate another process recording a buggy verdict while this
		// probe is in flight; AppArmorParser would now select abi/4.0.
		s.writeMarker(c, "1\n")
		c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)
		return true, nil
	})
	defer restore()

	c.Assert(apparmor.EnsureAppArmor5NetworkBugProbe(), IsNil)
	c.Check(gotParserArgs, DeepEquals, []string{
		mockParserCmd.Exe(),
		"--policy-features", abi50,
	})
	c.Check(apparmor.NetworkBugForcedAbi40(), Equals, true)
}

func (s *networkProbeSuite) TestProbeProfileSubstitutesProfileName(c *C) {
	// The template mentions ###NAME### in a comment before the actual
	// profile line; substituting only the first occurrence would leave
	// the profile named literally "###NAME###" and the change_onexec
	// transition would fail with "label not found".
	source, name := apparmor.NetworkBugProbeProfile()
	c.Check(strings.Contains(source, "###NAME###"), Equals, false)
	c.Check(strings.Contains(source, "profile "+name+" {"), Equals, true)
}

func (s *networkProbeSuite) TestAppArmorParserDowngradesOnMarkerDistro(c *C) {
	mockParserCmd := s.mockParserWithAbi50(c)
	abi40 := filepath.Join(s.fakeroot, "/etc/apparmor.d/abi/4.0")
	c.Assert(os.WriteFile(abi40, nil, 0644), IsNil)
	s.AddCleanup(apparmor.MockHostAbi40File(abi40))

	s.writeMarker(c, "1\n")
	cmd, internal, err := apparmor.AppArmorParser()
	c.Assert(err, IsNil)
	c.Check(internal, Equals, false)
	c.Check(cmd.Args, DeepEquals, []string{
		mockParserCmd.Exe(),
		"--policy-features", abi40,
	})
}

func (s *networkProbeSuite) TestAppArmorParserDowngradesOnMarkerInternal(c *C) {
	// internal parser with abi 3.0/4.0/5.0 present
	parser, libSnapdDir, restore := setupInternalAppArmorParserEnv(c, "3.0", "4.0", "5.0")
	s.AddCleanup(restore)

	s.writeMarker(c, "1\n")
	cmd, internal, err := apparmor.AppArmorParser()
	c.Assert(err, IsNil)
	c.Check(internal, Equals, true)
	c.Check(cmd.Args, DeepEquals, []string{
		parser,
		"--config-file", filepath.Join(libSnapdDir, "/apparmor/parser.conf"),
		"--base", filepath.Join(libSnapdDir, "/apparmor.d"),
		"--policy-features", filepath.Join(libSnapdDir, "/apparmor.d/abi/4.0"),
	})
}
