// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2024 Canonical Ltd
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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

func TestApparmor(t *testing.T) {
	TestingT(t)
}

type apparmorSuite struct {
	testutil.BaseTest
}

var _ = Suite(&apparmorSuite{})

func (s *apparmorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.AddCleanup(func() {
		configFile := filepath.Join(dirs.GlobalRootDir, "/etc/apparmor.d/tunables/home.d/snapd")
		if err := os.RemoveAll(configFile); err != nil {
			panic(err)
		}
	})
}

func (*apparmorSuite) TestAppArmorParser(c *C) {
	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore := apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return false })
	defer restore()
	cmd, internal, err := apparmor.AppArmorParser()
	c.Check(cmd.Path, Equals, mockParserCmd.Exe())
	c.Check(cmd.Args, DeepEquals, []string{mockParserCmd.Exe()})
	c.Check(internal, Equals, false)
	c.Check(err, Equals, nil)
}

func (*apparmorSuite) TestAppArmorInternalAppArmorParserAbi3(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	libSnapdDir := filepath.Join(dirs.SnapMountDir, "/snapd/42/usr/lib/snapd")
	parser := filepath.Join(libSnapdDir, "apparmor_parser")
	c.Assert(os.MkdirAll(libSnapdDir, 0755), IsNil)
	c.Assert(os.WriteFile(parser, nil, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(libSnapdDir, "apparmor.d/abi"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libSnapdDir, "apparmor.d/abi/3.0"), nil, 0644), IsNil)

	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(libSnapdDir, "snapd"), nil
	})
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return true })
	defer restore()

	cmd, internal, err := apparmor.AppArmorParser()
	c.Check(err, IsNil)
	c.Check(cmd.Path, Equals, parser)
	c.Check(cmd.Args, DeepEquals, []string{
		parser,
		"--config-file", filepath.Join(libSnapdDir, "/apparmor/parser.conf"),
		"--base", filepath.Join(libSnapdDir, "/apparmor.d"),
		"--policy-features", filepath.Join(libSnapdDir, "/apparmor.d/abi/3.0"),
	})
	c.Check(internal, Equals, true)
}

func (*apparmorSuite) TestAppArmorLevelTypeStringer(c *C) {
	c.Check(apparmor.Unknown.String(), Equals, "unknown")
	c.Check(apparmor.Unsupported.String(), Equals, "none")
	c.Check(apparmor.Unusable.String(), Equals, "unusable")
	c.Check(apparmor.Partial.String(), Equals, "partial")
	c.Check(apparmor.Full.String(), Equals, "full")
	c.Check(apparmor.LevelType(42).String(), Equals, "AppArmorLevelType:42")
}

func (*apparmorSuite) TestAppArmorSystemCacheFallsback(c *C) {
	// if we create the system cache dir under a new rootdir, then the
	// SystemCacheDir should take that value
	dir1 := c.MkDir()
	systemCacheDir := filepath.Join(dir1, "/etc/apparmor.d/cache")
	err := os.MkdirAll(systemCacheDir, 0755)
	c.Assert(err, IsNil)
	dirs.SetRootDir(dir1)
	c.Assert(apparmor.SystemCacheDir, Equals, systemCacheDir)

	// but if we set a new root dir without the system cache dir, now the var is
	// set to the CacheDir
	dir2 := c.MkDir()
	dirs.SetRootDir(dir2)
	c.Assert(apparmor.SystemCacheDir, Equals, apparmor.CacheDir)

	// finally test that it's insufficient to just have the conf dir, we need
	// specifically the cache dir
	dir3 := c.MkDir()
	err = os.MkdirAll(filepath.Join(dir3, "/etc/apparmor.d"), 0755)
	c.Assert(err, IsNil)
	dirs.SetRootDir(dir3)
	c.Assert(apparmor.SystemCacheDir, Equals, apparmor.CacheDir)
}

func (*apparmorSuite) TestMockAppArmorLevel(c *C) {
	for _, lvl := range []apparmor.LevelType{apparmor.Unsupported, apparmor.Unusable, apparmor.Partial, apparmor.Full} {
		restore := apparmor.MockLevel(lvl)
		c.Check(apparmor.ProbedLevel(), Equals, lvl)
		c.Check(apparmor.Summary(), testutil.Contains, "mocked apparmor level: ")
		features, err := apparmor.KernelFeatures()
		c.Check(err, IsNil)
		c.Check(features, DeepEquals, []string{"mocked-kernel-feature"})
		features, err = apparmor.ParserFeatures()
		c.Check(err, IsNil)
		c.Check(features, DeepEquals, []string{"mocked-parser-feature"})
		restore()
	}
}

// Using MockAppArmorFeatures yields in apparmor assessment
func (*apparmorSuite) TestMockAppArmorFeatures(c *C) {
	// No apparmor in the kernel, apparmor is disabled.
	restore := apparmor.MockFeatures([]string{}, os.ErrNotExist, []string{}, nil)
	c.Check(apparmor.ProbedLevel(), Equals, apparmor.Unsupported)
	c.Check(apparmor.Summary(), Equals, "apparmor not enabled")
	features, err := apparmor.KernelFeatures()
	c.Assert(err, Equals, os.ErrNotExist)
	c.Check(features, DeepEquals, []string{})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})
	restore()

	// No apparmor_parser, apparmor is disabled.
	restore = apparmor.MockFeatures([]string{}, nil, []string{}, os.ErrNotExist)
	c.Check(apparmor.ProbedLevel(), Equals, apparmor.Unsupported)
	c.Check(apparmor.Summary(), Equals, "apparmor_parser not found")
	features, err = apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, Equals, os.ErrNotExist)
	c.Check(features, DeepEquals, []string{})
	restore()

	// Complete kernel features but apparmor is unusable because of missing required parser features.
	restore = apparmor.MockFeatures(apparmor.RequiredKernelFeatures, nil, []string{}, nil)
	c.Check(apparmor.ProbedLevel(), Equals, apparmor.Unusable)
	c.Check(apparmor.Summary(), Equals, "apparmor_parser is available but required parser features are missing: unsafe")
	features, err = apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, apparmor.RequiredKernelFeatures)
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})
	restore()

	// Complete parser features but apparmor is unusable because of missing required kernel features.
	// The test feature is there to pretend that apparmor in the kernel is not entirely disabled.
	restore = apparmor.MockFeatures([]string{"test-feature"}, nil, apparmor.RequiredParserFeatures, nil)
	c.Check(apparmor.ProbedLevel(), Equals, apparmor.Unusable)
	c.Check(apparmor.Summary(), Equals, "apparmor is enabled but required kernel features are missing: file")
	features, err = apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"test-feature"})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, apparmor.RequiredParserFeatures)
	restore()

	// Required kernel and parser features available, some optional features are missing though.
	restore = apparmor.MockFeatures(apparmor.RequiredKernelFeatures, nil, apparmor.RequiredParserFeatures, nil)
	c.Check(apparmor.ProbedLevel(), Equals, apparmor.Partial)
	c.Check(apparmor.Summary(), Equals, "apparmor is enabled but some kernel features are missing: caps, dbus, domain, mount, namespaces, network, ptrace, signal")
	features, err = apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, apparmor.RequiredKernelFeatures)
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, apparmor.RequiredParserFeatures)
	restore()

	// Preferred kernel and parser features available.
	restore = apparmor.MockFeatures(apparmor.PreferredKernelFeatures, nil, apparmor.PreferredParserFeatures, nil)
	c.Check(apparmor.ProbedLevel(), Equals, apparmor.Full)
	c.Check(apparmor.Summary(), Equals, "apparmor is enabled and all features are available")
	features, err = apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, apparmor.PreferredKernelFeatures)
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, apparmor.PreferredParserFeatures)
	restore()
}

const featuresSysPath = "sys/kernel/security/apparmor/features"

func (s *apparmorSuite) TestProbeAppArmorKernelFeatures(c *C) {
	d := c.MkDir()

	// Pretend that apparmor kernel features directory doesn't exist.
	restore := apparmor.MockFsRootPath(d)
	defer restore()
	features, err := apparmor.ProbeKernelFeatures()
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Check(features, DeepEquals, []string{})

	// Pretend that apparmor kernel features directory exists but is empty.
	c.Assert(os.MkdirAll(filepath.Join(d, featuresSysPath), 0755), IsNil)
	features, err = apparmor.ProbeKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})

	// Pretend that apparmor kernel features directory contains some entries.
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "foo"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "bar"), 0755), IsNil)
	features, err = apparmor.ProbeKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"bar", "foo"})

	// Also test sub-features features
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "foo", "baz"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "foo", "qux"), 0755), IsNil)
	features, err = apparmor.ProbeKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"bar", "foo", "foo:baz", "foo:qux"})
}

func probeOneParserFeature(c *C, known *[]string, parserPath, featureName, profileText string) {
	probeOneVersionDependentParserFeature(c, known, parserPath, "0.0.0", featureName, profileText)
}

// fakeParserScript returns a shell script mimicking apparmor_parser.
//
// The returned script is prepared such, that additional logic may be safely
// appended to it.
func fakeParserScript(parserVersion string) string {
	const script = `#!/bin/sh
set -e
if [ "${1:-}" = "--version" ]; then
  cat <<__VERSION__
AppArmor parser version %s
Copyright (C) 1999-2008 Novell Inc.
Copyright 2009-2018 Canonical Ltd.
__VERSION__
  exit 0
fi
`
	return fmt.Sprintf(script, parserVersion)
}

func fakeParserAnticipatingProfileScript(parserVersion, profileText string) string {
	textCompareLogic := fmt.Sprintf(`test "$(cat | tr -d '\n')" = '%s'`, profileText)

	return fakeParserScript(parserVersion) + textCompareLogic
}

func probeOneVersionDependentParserFeature(c *C, known *[]string, parserPath, parserVersion, featureName, profileText string) {
	err := os.WriteFile(parserPath, []byte(fakeParserAnticipatingProfileScript(parserVersion, profileText)), 0o700)
	c.Assert(err, IsNil)

	cmd, _, _ := apparmor.AppArmorParser()
	c.Assert(cmd.Path, Equals, parserPath, Commentf("Unexpectedly using apparmor parser from %s", cmd.Path))

	features, err := apparmor.ProbeParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{featureName},
		Commentf("Unexpected features detected for profile text: %s", profileText))

	*known = append(*known, featureName)
}

type parserFeatureTestSuite struct {
	testutil.BaseTest
	d      string
	binDir string
}

var _ = Suite(&parserFeatureTestSuite{})

func (s *parserFeatureTestSuite) SetUpTest(c *C) {
	s.d = c.MkDir()
	// This is used to find related parser files and isolates us from the host.
	dirs.SetRootDir(s.d)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.binDir = filepath.Join(s.d, "bin")
	err := os.Mkdir(s.binDir, 0o755)
	c.Assert(err, IsNil)

	restore := apparmor.MockParserSearchPath(s.binDir)
	s.AddCleanup(restore)
}

func (s *parserFeatureTestSuite) TestProbeMqueueWith4Beta(c *C) {
	const parserVersion = "4.0.0~beta3"
	const profileText = `profile snap-test { mqueue,}`

	parserPath := filepath.Join(s.binDir, "apparmor_parser")

	err := os.WriteFile(parserPath, []byte(fakeParserAnticipatingProfileScript(parserVersion, profileText)), 0o700)
	c.Assert(err, IsNil)

	cmd, _, _ := apparmor.AppArmorParser()
	c.Assert(cmd.Path, Equals, parserPath, Commentf("Unexpectedly using apparmor parser from %s", cmd.Path))

	features, err := apparmor.ProbeParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, HasLen, 0, Commentf("Mqueue feature unexpectedly enabled by fake 4.0.0~beta3 parser"))
}

func (s *parserFeatureTestSuite) TestProbeAllowAllWith4_0_1(c *C) {
	const parserVersion = "4.0.1"
	const profileText = `profile snap-test { allow all,}`

	parserPath := filepath.Join(s.binDir, "apparmor_parser")

	err := os.WriteFile(parserPath, []byte(fakeParserAnticipatingProfileScript(parserVersion, profileText)), 0o700)
	c.Assert(err, IsNil)

	cmd, _, _ := apparmor.AppArmorParser()
	c.Assert(cmd.Path, Equals, parserPath, Commentf("Unexpectedly using apparmor parser from %s", cmd.Path))

	features, err := apparmor.ProbeParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, HasLen, 0, Commentf("Allow all unexpectedly enabled by fake 4.0.1 parser"))
}

func (s *parserFeatureTestSuite) TestProbeFeature(c *C) {
	// Pretend we can only support one feature at a time.
	var knownProbes []string

	parserPath := filepath.Join(s.binDir, "apparmor_parser")

	probeOneVersionDependentParserFeature(c, &knownProbes, parserPath, "4.0.2", "allow-all", `profile snap-test { allow all,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "cap-audit-read", `profile snap-test { capability audit_read,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "cap-bpf", `profile snap-test { capability bpf,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "include-if-exists", `profile snap-test { #include if exists "/foo"}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "io-uring", `profile snap-test { allow io_uring,}`)
	probeOneVersionDependentParserFeature(c, &knownProbes, parserPath, "4.0.1", "mqueue", `profile snap-test { mqueue,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "qipcrtr-socket", `profile snap-test { network qipcrtr dgram,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "unconfined", `profile snap-test flags=(unconfined) { # test unconfined}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "unsafe", `profile snap-test { change_profile unsafe /**,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "userns", `profile snap-test { userns,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "xdp", `profile snap-test { network xdp,}`)

	// Pretend we have all the features.
	err := os.WriteFile(parserPath, []byte(fakeParserScript("4.0.2")), 0o755)
	c.Assert(err, IsNil)

	// Did any feature probes got added to non-test code?
	features, err := apparmor.ProbeParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, knownProbes, Commentf("Additional probes added not reflected in test code"))
}

func (s *parserFeatureTestSuite) TestNoParser(c *C) {
	// Pretend we don't have any apparmor_parser at all.
	features, err := apparmor.ProbeParserFeatures()
	if !errors.Is(err, os.ErrNotExist) {
		c.Fatal("Unexpected error", err)
	}
	// Did we get any features?
	c.Check(features, HasLen, 0, Commentf("Unexpected features without any parser"))
}

func (s *parserFeatureTestSuite) TestInternalParser(c *C) {
	// Put a fake parser at $SNAP_MOUNT_DIR/snapd/42/usr/lib/snapd/apparmor_parser
	libSnapdDir := filepath.Join(dirs.SnapMountDir, "/snapd/42/usr/lib/snapd")
	parser := filepath.Join(libSnapdDir, "apparmor_parser")
	c.Assert(os.MkdirAll(libSnapdDir, 0755), IsNil)
	c.Assert(os.WriteFile(parser, nil, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(libSnapdDir, "apparmor.d/abi"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libSnapdDir, "apparmor.d/abi/4.0"), nil, 0644), IsNil)

	// Pretend that we are running snapd from that snap location.
	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		if path != "/proc/self/exe" {
			c.Fatal("Unexpected readlink", path)
		}

		return filepath.Join(libSnapdDir, "snapd"), nil
	})
	s.AddCleanup(restore)

	// Pretend snapd supports re-execution from snapd snap.
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return true })
	s.AddCleanup(restore)

	// Did we recognize the internal parser?
	features, err := apparmor.ProbeParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"snapd-internal"})
}

func (s *apparmorSuite) TestInterfaceSystemKey(c *C) {
	apparmor.FreshAppArmorAssessment()

	d := c.MkDir()
	restore := apparmor.MockFsRootPath(d)
	defer restore()
	c.Assert(os.MkdirAll(filepath.Join(d, featuresSysPath, "policy"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, featuresSysPath, "network"), 0755), IsNil)

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", fakeParserScript("4.0.1"))
	defer mockParserCmd.Restore()
	restore = apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	apparmor.ProbedLevel()

	features, err := apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"network", "policy"})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "io-uring", "mqueue", "qipcrtr-socket", "unconfined", "unsafe", "userns", "xdp"})
}

func (s *apparmorSuite) TestAppArmorParserMtime(c *C) {
	// Pretend that we have apparmor_parser.
	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore := apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()
	mtime := apparmor.ParserMtime()
	fi, err := os.Stat(filepath.Join(mockParserCmd.BinDir(), "apparmor_parser"))
	c.Assert(err, IsNil)
	c.Check(mtime, Equals, fi.ModTime().Unix())

	// Pretend that we don't have apparmor_parser.
	restore = apparmor.MockParserSearchPath(c.MkDir())
	defer restore()
	mtime = apparmor.ParserMtime()
	c.Check(mtime, Equals, int64(0))
}

func (s *apparmorSuite) TestFeaturesProbedOnce(c *C) {
	apparmor.FreshAppArmorAssessment()

	d := c.MkDir()
	restore := apparmor.MockFsRootPath(d)
	defer restore()
	c.Assert(os.MkdirAll(filepath.Join(d, featuresSysPath, "policy"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, featuresSysPath, "network"), 0755), IsNil)

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", fakeParserScript("4.0.1"))
	defer mockParserCmd.Restore()
	restore = apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	features, err := apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"network", "policy"})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "io-uring", "mqueue", "qipcrtr-socket", "unconfined", "unsafe", "userns", "xdp"})

	// this makes probing fails but is not done again
	err = os.RemoveAll(d)
	c.Assert(err, IsNil)

	_, err = apparmor.KernelFeatures()
	c.Assert(err, IsNil)

	// this makes probing fails but is not done again
	err = os.RemoveAll(mockParserCmd.BinDir())
	c.Assert(err, IsNil)

	_, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
}

func (s *apparmorSuite) TestValidateFreeFromAAREUnhappy(c *C) {
	var testCases = []string{"a?", "*b", "c[c", "dd]", "e{", "f}", "g^", `h"`, "f\000", "g\x00"}

	for _, s := range testCases {
		c.Check(apparmor.ValidateNoAppArmorRegexp(s), ErrorMatches, ".* contains a reserved apparmor char from .*", Commentf("%q is not raising an error", s))
	}
}

func (s *apparmorSuite) TestValidateFreeFromAAREhappy(c *C) {
	var testCases = []string{"foo", "BaR", "b-z", "foo+bar", "b00m!", "be/ep", "a%b", "a&b", "a(b", "a)b", "a=b", "a#b", "a~b", "a'b", "a_b", "a,b", "a;b", "a>b", "a<b", "a|b"}

	for _, s := range testCases {
		c.Check(apparmor.ValidateNoAppArmorRegexp(s), IsNil, Commentf("%q raised an error but shouldn't", s))
	}
}

func (s *apparmorSuite) TestUpdateHomedirsTunableMkdirFail(c *C) {
	restore := apparmor.MockMkdirAll(func(string, os.FileMode) error {
		return errors.New("mkdir failure")
	})
	defer restore()

	err := apparmor.UpdateHomedirsTunable([]string{"does", "not", "matter"})
	c.Check(err, ErrorMatches, `cannot create AppArmor tunable directory: mkdir failure`)
}

func (s *apparmorSuite) TestUpdateHomedirsTunableWriteFail(c *C) {
	restore := apparmor.MockMkdirAll(func(string, os.FileMode) error {
		return nil
	})
	defer restore()

	restore = apparmor.MockAtomicWrite(func(string, io.Reader, os.FileMode, osutil.AtomicWriteFlags) error {
		return errors.New("write failure")
	})
	defer restore()

	err := apparmor.UpdateHomedirsTunable([]string{"does", "not", "matter"})
	c.Check(err, ErrorMatches, `write failure`)
}

func (s *apparmorSuite) TestUpdateHomedirsTunableHappy(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	err := apparmor.UpdateHomedirsTunable([]string{"/home/a", "/dir2"})
	c.Assert(err, IsNil)
	configFile := filepath.Join(dirs.GlobalRootDir, "/etc/apparmor.d/tunables/home.d/snapd")
	fileContents, err := os.ReadFile(configFile)
	c.Assert(err, IsNil)
	c.Check(string(fileContents), Equals,
		`# Generated by snapd -- DO NOT EDIT!`+"\n"+`@{HOMEDIRS}+="/home/a" "/dir2"`+"\n")
}

func (s *apparmorSuite) TestUpdateHomedirsTunableHappyNoDirs(c *C) {
	err := apparmor.UpdateHomedirsTunable([]string{})
	c.Check(err, IsNil)
	configFile := filepath.Join(dirs.GlobalRootDir, "/etc/apparmor.d/tunables/home.d/snapd")
	c.Check(osutil.FileExists(configFile), Equals, false)
}

func (s *apparmorSuite) TestSnapdAppArmorSupportsReexecImpl(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	// with no info file should indicate it does not support reexec
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)

	d := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	c.Assert(os.MkdirAll(d, 0755), IsNil)
	infoFile := filepath.Join(d, "info")
	c.Assert(os.WriteFile(infoFile, []byte("VERSION=foo"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)
	c.Assert(os.WriteFile(infoFile, []byte("VERSION=foo\nSNAPD_APPARMOR_REEXEC=0"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)
	c.Assert(os.WriteFile(infoFile, []byte("VERSION=foo\nSNAPD_APPARMOR_REEXEC=foo"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)
	c.Assert(os.WriteFile(infoFile, []byte("VERSION=foo\nSNAPD_APPARMOR_REEXEC=1"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, true)
}

func (s *apparmorSuite) TestSetupConfCacheDirs(c *C) {
	apparmor.SetupConfCacheDirs("/newdir")
	c.Check(apparmor.SnapConfineAppArmorDir, Equals, "/newdir/var/lib/snapd/apparmor/snap-confine")
}

func (s *apparmorSuite) TestSetupConfCacheDirsWithInternalApparmor(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	libSnapdDir := filepath.Join(dirs.SnapMountDir, "/snapd/42/usr/lib/snapd")
	parser := filepath.Join(libSnapdDir, "apparmor_parser")
	c.Assert(os.MkdirAll(libSnapdDir, 0755), IsNil)
	c.Assert(os.WriteFile(parser, nil, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(libSnapdDir, "apparmor.d/abi"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libSnapdDir, "apparmor.d/abi/4.0"), nil, 0644), IsNil)
	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(libSnapdDir, "snapd"), nil
	})
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return true })
	defer restore()

	apparmor.SetupConfCacheDirs("/newdir")
	c.Check(apparmor.SnapConfineAppArmorDir, Equals, "/newdir/var/lib/snapd/apparmor/snap-confine.internal")
}

func (s *apparmorSuite) TestSystemAppArmorLoadsSnapPolicyErr(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)
	fakeApparmorFunctionsPath := filepath.Join(fakeroot, "/lib/apparmor/functions")
	err := os.MkdirAll(filepath.Dir(fakeApparmorFunctionsPath), 0750)
	c.Assert(err, IsNil)

	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	log, restore := logger.MockLogger()
	defer restore()

	// no log output on missing file
	c.Check(apparmor.SystemAppArmorLoadsSnapPolicy(), Equals, false)
	c.Check(log.String(), Equals, "")

	// permissions are ignored as root
	if os.Getuid() == 0 {
		return
	}
	// log generated for errors
	err = os.WriteFile(fakeApparmorFunctionsPath, nil, 0100)
	c.Assert(err, IsNil)
	c.Check(apparmor.SystemAppArmorLoadsSnapPolicy(), Equals, false)
	c.Check(log.String(), Matches, `(?ms).* DEBUG: cannot open apparmor functions file: open .*/lib/apparmor/functions: permission denied`)
}

func (s *apparmorSuite) TestSystemAppArmorLoadsSnapPolicy(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	// systemAppArmorLoadsSnapPolicy() will look at this path so it
	// needs to be the real path, not a faked one
	dirs.SnapAppArmorDir = dirs.SnapAppArmorDir[len(fakeroot):]

	fakeApparmorFunctionsPath := filepath.Join(fakeroot, "/lib/apparmor/functions")
	err := os.MkdirAll(filepath.Dir(fakeApparmorFunctionsPath), 0755)
	c.Assert(err, IsNil)

	for _, tc := range []struct {
		apparmorFunctionsContent string
		expectedResult           bool
	}{
		{"", false},
		{"unrelated content", false},
		// 16.04
		{`PROFILES_SNAPPY="/var/lib/snapd/apparmor/profiles"`, true},
		// 18.04
		{`PROFILES_VAR="/var/lib/snapd/apparmor/profiles"`, true},
	} {
		err := os.WriteFile(fakeApparmorFunctionsPath, []byte(tc.apparmorFunctionsContent), 0644)
		c.Assert(err, IsNil)

		loadsPolicy := apparmor.SystemAppArmorLoadsSnapPolicy()
		c.Check(loadsPolicy, Equals, tc.expectedResult, Commentf("%v", tc))
	}
}
