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

func (*apparmorSuite) TestAppArmorInternalAppArmorParser(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	d := filepath.Join(dirs.SnapMountDir, "/snapd/42", "/usr/lib/snapd")
	c.Assert(os.MkdirAll(d, 0755), IsNil)
	p := filepath.Join(d, "apparmor_parser")
	c.Assert(os.WriteFile(p, nil, 0755), IsNil)
	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(d, "snapd"), nil
	})
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return true })
	defer restore()

	cmd, internal, err := apparmor.AppArmorParser()
	c.Check(err, IsNil)
	c.Check(cmd.Path, Equals, p)
	c.Check(cmd.Args, DeepEquals, []string{
		p,
		"--config-file", filepath.Join(d, "/apparmor/parser.conf"),
		"--base", filepath.Join(d, "/apparmor.d"),
		"--policy-features", filepath.Join(d, "/apparmor.d/abi/3.0"),
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
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "xyz"), 0755), IsNil)
	features, err = apparmor.ProbeKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"bar", "foo", "xyz"})

	// Also test sub-features features
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "foo", "baz"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "foo", "qux"), 0755), IsNil)
	features, err = apparmor.ProbeKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"bar", "foo", "foo:baz", "foo:qux", "xyz"})

	// Also test that prompt feature is read from permstable32 if it exists
	c.Assert(os.Mkdir(filepath.Join(d, featuresSysPath, "policy"), 0755), IsNil)
	for _, testCase := range []struct {
		permstableContent string
		expectedSuffixes  []string
	}{
		{
			"allow deny prompt fizz buzz",
			[]string{"allow", "buzz", "deny", "fizz", "prompt"},
		},
		{
			"allow  deny prompt fizz   buzz ",
			[]string{"allow", "buzz", "deny", "fizz", "prompt"},
		},
		{
			"allow  deny\nprompt fizz \n  buzz",
			[]string{"allow", "buzz", "deny", "fizz", "prompt"},
		},
		{
			"allow  deny\nprompt fizz \n  buzz\n ",
			[]string{"allow", "buzz", "deny", "fizz", "prompt"},
		},
		{
			"fizz",
			[]string{"fizz"},
		},
		{
			"\n\n\nfizz\n\nbuzz",
			[]string{"buzz", "fizz"},
		},
		{
			"fizz\tbuzz",
			[]string{"buzz", "fizz"},
		},
		{
			"fizz buzz\r\n",
			[]string{"buzz", "fizz"},
		},
	} {
		c.Assert(os.WriteFile(filepath.Join(d, featuresSysPath, "policy", "permstable32"), []byte(testCase.permstableContent), 0644), IsNil)
		features, err = apparmor.ProbeKernelFeatures()
		c.Assert(err, IsNil)
		expected := []string{"bar", "foo", "foo:baz", "foo:qux", "policy"}
		for _, suffix := range testCase.expectedSuffixes {
			expected = append(expected, fmt.Sprintf("policy:permstable32:%s", suffix))
		}
		expected = append(expected, "xyz")
		c.Check(features, DeepEquals, expected, Commentf("test case: %+v", testCase))
	}
}

func probeOneParserFeature(c *C, known *[]string, parserPath, featureName, profileText string) {
	const script = `#!/bin/sh
set -e
test "$(cat | tr -d '\n')" = '%s'
`
	err := os.WriteFile(parserPath, []byte(fmt.Sprintf(script, profileText)), 0o755)
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

func (s *parserFeatureTestSuite) TestProbeFeature(c *C) {
	// Pretend we can only support one feature at a time.
	var knownProbes []string

	parserPath := filepath.Join(s.binDir, "apparmor_parser")

	probeOneParserFeature(c, &knownProbes, parserPath, "cap-audit-read", `profile snap-test { capability audit_read,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "cap-bpf", `profile snap-test { capability bpf,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "include-if-exists", `profile snap-test { #include if exists "/foo"}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "mqueue", `profile snap-test { mqueue,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "prompt", `profile snap-test { prompt /foo r,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "qipcrtr-socket", `profile snap-test { network qipcrtr dgram,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "unconfined", `profile snap-test flags=(unconfined) { # test unconfined}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "unsafe", `profile snap-test { change_profile unsafe /**,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "userns", `profile snap-test { userns,}`)
	probeOneParserFeature(c, &knownProbes, parserPath, "xdp", `profile snap-test { network xdp,}`)

	// Pretend we have all the features.
	err := os.WriteFile(parserPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)
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
	err := os.MkdirAll(libSnapdDir, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(libSnapdDir, "apparmor_parser"), nil, 0755)
	c.Assert(err, IsNil)

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

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore = apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	apparmor.ProbedLevel()

	features, err := apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"network", "policy"})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "mqueue", "prompt", "qipcrtr-socket", "unconfined", "unsafe", "userns", "xdp"})
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

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore = apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	features, err := apparmor.KernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"network", "policy"})
	features, err = apparmor.ParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "mqueue", "prompt", "qipcrtr-socket", "unconfined", "unsafe", "userns", "xdp"})

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

func (s *apparmorSuite) TestPromptingSupported(c *C) {
	goodKernelFeatures := []string{"policy:permstable32:prompt"}
	goodParserFeatures := []string{"prompt"}

	for _, testCase := range []struct {
		kernelFeatures []string
		kernelError    error
		parserFeatures []string
		parserError    error
		expectedReason string
	}{
		{
			kernelFeatures: []string{},
			kernelError:    fmt.Errorf("foo"),
			parserFeatures: []string{},
			parserError:    fmt.Errorf("bar"),
			expectedReason: "cannot check apparmor kernel features: foo",
		},
		{
			kernelFeatures: []string{"policy:permstable32:allow", "policy:permstable32:deny", "policy:permstable32:prompt"},
			kernelError:    nil,
			parserFeatures: []string{},
			parserError:    fmt.Errorf("bar"),
			expectedReason: "cannot check apparmor parser features: bar",
		},
		{
			kernelFeatures: []string{"policy:permstable32:allow", "policy:permstable32:deny"},
			kernelError:    nil,
			parserFeatures: []string{},
			parserError:    nil,
			expectedReason: "apparmor kernel features do not support prompting",
		},
		{
			kernelFeatures: []string{"policy:permstable32:allow", "policy:permstable32:deny", "policy:permstable32:prompt"},
			kernelError:    nil,
			parserFeatures: []string{"mqueue"},
			parserError:    nil,
			expectedReason: "apparmor parser does not support the prompt qualifier",
		},
	} {
		restore := apparmor.MockFeatures(testCase.kernelFeatures, testCase.kernelError, testCase.parserFeatures, testCase.parserError)
		supported, reason := apparmor.PromptingSupported()
		c.Check(supported, Equals, false)
		c.Check(reason, Equals, testCase.expectedReason)
		restore()
	}

	restore := apparmor.MockFeatures(goodKernelFeatures, nil, goodParserFeatures, nil)
	defer restore()

	supported, reason := apparmor.PromptingSupported()
	// TODO: change this once snapd supports prompting
	c.Check(supported, Equals, false)
	c.Check(reason, Equals, "requires newer version of snapd")
	// c.Check(supported, Equals, true)
	// c.Check(reason, Equals, "")
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

	d := filepath.Join(dirs.SnapMountDir, "/snapd/42", "/usr/lib/snapd")
	c.Assert(os.MkdirAll(d, 0755), IsNil)
	p := filepath.Join(d, "apparmor_parser")
	c.Assert(os.WriteFile(p, nil, 0755), IsNil)
	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(d, "snapd"), nil
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
