// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
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

func (s *apparmorSuite) TestProbeAppArmorParserFeatures(c *C) {
	var features = []string{"unsafe", "include-if-exists", "qipcrtr-socket", "mqueue", "cap-bpf", "cap-audit-read", "xdp", "userns", "unconfined"}
	// test all combinations of features
	for i := 0; i < int(math.Pow(2, float64(len(features)))); i++ {
		expFeatures := []string{}
		d := c.MkDir()
		contents := ""
		var expectedCalls [][]string
		for j, f := range features {
			code := 0
			if i&(1<<j) == 0 {
				expFeatures = append([]string{f}, expFeatures...)
			} else {
				code = 1
			}
			expectedCalls = append(expectedCalls, []string{"apparmor_parser", "--preprocess"})
			contents += fmt.Sprintf("%d ", code)
		}
		// probeParserFeatures() sorts the features
		sort.Strings(expFeatures)
		err := os.WriteFile(filepath.Join(d, "codes"), []byte(contents), 0755)
		c.Assert(err, IsNil)
		mockParserCmd := testutil.MockCommand(c, "apparmor_parser", fmt.Sprintf(`
cat >> %[1]s/stdin
echo "" >> %[1]s/stdin

read -r EXIT_CODE CODES_FOR_NEXT_CALLS < %[1]s/codes
echo "$CODES_FOR_NEXT_CALLS" > %[1]s/codes

exit "$EXIT_CODE"
`, d))
		defer mockParserCmd.Restore()
		restore := apparmor.MockParserSearchPath(mockParserCmd.BinDir())
		defer restore()

		features, err := apparmor.ProbeParserFeatures()
		c.Assert(err, IsNil)
		if len(expFeatures) == 0 {
			c.Check(features, HasLen, 0)
		} else {
			c.Check(features, DeepEquals, expFeatures)
		}

		c.Check(mockParserCmd.Calls(), DeepEquals, expectedCalls)
		data, err := ioutil.ReadFile(filepath.Join(d, "stdin"))
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, `profile snap-test {
 change_profile unsafe /**,
}
profile snap-test {
 #include if exists "/foo"
}
profile snap-test {
 network qipcrtr dgram,
}
profile snap-test {
 mqueue,
}
profile snap-test {
 capability bpf,
}
profile snap-test {
 capability audit_read,
}
profile snap-test {
 network xdp,
}
profile snap-test {
 userns,
}
profile snap-test flags=(unconfined) {
 # test unconfined
}
`)
	}

	// Pretend that we just don't have apparmor_parser at all.
	restore := apparmor.MockParserSearchPath(c.MkDir())
	defer restore()
	features, err := apparmor.ProbeParserFeatures()
	c.Check(err, Equals, os.ErrNotExist)
	c.Check(features, DeepEquals, []string{})

	// pretend we have an internal apparmor_parser
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	d := filepath.Join(dirs.SnapMountDir, "/snapd/42", "/usr/lib/snapd")
	c.Assert(os.MkdirAll(d, 0755), IsNil)
	p := filepath.Join(d, "apparmor_parser")
	c.Assert(os.WriteFile(p, nil, 0755), IsNil)
	restore = snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(d, "snapd"), nil
	})
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return true })
	defer restore()
	features, err = apparmor.ProbeParserFeatures()
	c.Check(err, Equals, nil)
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
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "mqueue", "qipcrtr-socket", "unconfined", "unsafe", "userns", "xdp"})
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
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "mqueue", "qipcrtr-socket", "unconfined", "unsafe", "userns", "xdp"})

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
	fileContents, err := ioutil.ReadFile(configFile)
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

func (s *apparmorSuite) TestGenerateAAREExclusionPatterns(c *C) {
	tt := []struct {
		comment         string
		excludePatterns []string
		prefix          string
		suffix          string
		err             string
		expRule         string
	}{
		// simple single pattern cases
		{
			comment:         "single shortest",
			excludePatterns: []string{"/ab"},
			prefix:          "pre ",
			suffix:          " suf",
			expRule: `
pre /[^a]** suf
pre /a[^b]** suf
`[1:],
		},
		{
			comment:         "single simple with wildcard",
			excludePatterns: []string{"/a/*/bc"},
			expRule: `
/[^a]**
/a[^/]**
/a/*/[^b]**
/a/*/b[^c]**
`[1:],
		},

		// multiple pattern cases
		{
			comment:         "same length no overlap shortest",
			excludePatterns: []string{"/a", "/d"},
			expRule: `
/[^ad]**
`[1:],
		},
		{
			comment:         "diff length no overlap shortest",
			excludePatterns: []string{"/a", "/dc"},
			expRule: `
/[^ad]**
/d[^c]**
`[1:],
		},
		{
			comment:         "diff length overlap",
			excludePatterns: []string{"/ad", "/adc"},
			expRule: `
/[^a]**
/a[^d]**
/ad[^c]**
`[1:],
		},
		{
			comment:         "same length no overlap",
			excludePatterns: []string{"/ab", "/de"},
			expRule: `
/[^ad]**
/{a[^b],d[^e]}**
`[1:],
		},
		{
			comment:         "same length overlap",
			excludePatterns: []string{"/ab", "/ac"},
			expRule: `
/[^a]**
/a[^bc]**
`[1:],
		},
		{
			comment:         "same length overlap",
			excludePatterns: []string{"/AB", "/AC"},
			expRule: `
/[^A]**
/A[^BC]**
`[1:],
		},
		{
			comment:         "same length overlap with wildcard",
			excludePatterns: []string{"/ab/*/c", "/ad/*/c"},
			expRule: `
/[^a]**
/a[^bd]**
/a{b[^/],d[^/]}**
/a{b/*/[^c],d/*/[^c]}**
`[1:],
		},
		{
			comment: "different length same overlap with extra nonoverlapping",
			// this is special because if we weren't using an OrderedSet and
			// instead had a []rune in the negCharsByAllowChars impl, we would
			// get rules like
			// /a/{b[^cc],f[^g]}
			// instead of the correct one which is de-duplicated like
			// /a/{b[^c],f[^g]}
			// apparmor doesn't like the former and only accepts the latter
			excludePatterns: []string{"/a/bc/d", "/a/bc/e", "/a/fg"},
			expRule: `
/[^a]**
/a[^/]**
/a/[^bf]**
/a/{b[^c],f[^g]}**
/a/bc[^/]**
/a/bc/[^de]**
`[1:],
		},
		{
			comment:         "diff length overlap with wildcard",
			excludePatterns: []string{"/abc/*/c", "/ad/*/c"},
			expRule: `
/[^a]**
/a[^bd]**
/a{b[^c],d[^/]}**
/abc[^/]**
/ad/*/[^c]**
/abc/*/[^c]**
`[1:],
		},
		{
			comment:         "more diff length overlap with wildcard",
			excludePatterns: []string{"/abc/*/c", "/ab/*/e", "/ad/*/c"},
			expRule: `
/[^a]**
/a[^bd]**
/a{b[^c/],d[^/]}**
/abc[^/]**
/a{b/*/[^e],d/*/[^c]}**
/abc/*/[^c]**
`[1:],
		},
		{
			comment:         "very diff length overlap with wildcard after diff length",
			excludePatterns: []string{"/abc/*/c/*/e", "/ad/*/c"},
			expRule: `
/[^a]**
/a[^bd]**
/a{b[^c],d[^/]}**
/abc[^/]**
/ad/*/[^c]**
/abc/*/[^c]**
/abc/*/c[^/]**
/abc/*/c/*/[^e]**
`[1:],
		},
		{
			comment:         "same length overlap with wildcard in overlap",
			excludePatterns: []string{"/a/*/b", "/a/*/c"},
			expRule: `
/[^a]**
/a[^/]**
/a/*/[^bc]**
`[1:],
		},

		// unhappy cases
		{
			comment:         "duplicates",
			excludePatterns: []string{"/ab/*/c", "/ad/*/c", "/ad/*/c"},
			err:             "exclude patterns contain duplicates",
		},
		{
			comment:         "nothing",
			excludePatterns: []string{},
			err:             "no patterns provided",
		},
		{
			comment:         "relative path",
			excludePatterns: []string{"foo"},
			err:             "exclude patterns must be absolute filepaths",
		},

		// specific cases used around codebase
		{
			comment: "any file inherit exec rules except snap-confine for devmode snap executing other snaps",
			excludePatterns: []string{
				"/snap/core/*/usr/lib/snapd/snap-confine",
				"/snap/snapd/*/usr/lib/snapd/snap-confine",
			},
			prefix: "",
			suffix: " rwlix,",
			expRule: `
/[^s]** rwlix,
/s[^n]** rwlix,
/sn[^a]** rwlix,
/sna[^p]** rwlix,
/snap[^/]** rwlix,
/snap/[^cs]** rwlix,
/snap/{c[^o],s[^n]}** rwlix,
/snap/{co[^r],sn[^a]}** rwlix,
/snap/{cor[^e],sna[^p]}** rwlix,
/snap/{core[^/],snap[^d]}** rwlix,
/snap/snapd[^/]** rwlix,
/snap/core/*/[^u]** rwlix,
/snap/{core/*/u[^s],snapd/*/[^u]}** rwlix,
/snap/{core/*/us[^r],snapd/*/u[^s]}** rwlix,
/snap/{core/*/usr[^/],snapd/*/us[^r]}** rwlix,
/snap/{core/*/usr/[^l],snapd/*/usr[^/]}** rwlix,
/snap/{core/*/usr/l[^i],snapd/*/usr/[^l]}** rwlix,
/snap/{core/*/usr/li[^b],snapd/*/usr/l[^i]}** rwlix,
/snap/{core/*/usr/lib[^/],snapd/*/usr/li[^b]}** rwlix,
/snap/{core/*/usr/lib/[^s],snapd/*/usr/lib[^/]}** rwlix,
/snap/{core/*/usr/lib/s[^n],snapd/*/usr/lib/[^s]}** rwlix,
/snap/{core/*/usr/lib/sn[^a],snapd/*/usr/lib/s[^n]}** rwlix,
/snap/{core/*/usr/lib/sna[^p],snapd/*/usr/lib/sn[^a]}** rwlix,
/snap/{core/*/usr/lib/snap[^d],snapd/*/usr/lib/sna[^p]}** rwlix,
/snap/{core/*/usr/lib/snapd[^/],snapd/*/usr/lib/snap[^d]}** rwlix,
/snap/{core/*/usr/lib/snapd/[^s],snapd/*/usr/lib/snapd[^/]}** rwlix,
/snap/{core/*/usr/lib/snapd/s[^n],snapd/*/usr/lib/snapd/[^s]}** rwlix,
/snap/{core/*/usr/lib/snapd/sn[^a],snapd/*/usr/lib/snapd/s[^n]}** rwlix,
/snap/{core/*/usr/lib/snapd/sna[^p],snapd/*/usr/lib/snapd/sn[^a]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap[^-],snapd/*/usr/lib/snapd/sna[^p]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-[^c],snapd/*/usr/lib/snapd/snap[^-]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-c[^o],snapd/*/usr/lib/snapd/snap-[^c]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-co[^n],snapd/*/usr/lib/snapd/snap-c[^o]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-con[^f],snapd/*/usr/lib/snapd/snap-co[^n]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-conf[^i],snapd/*/usr/lib/snapd/snap-con[^f]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-confi[^n],snapd/*/usr/lib/snapd/snap-conf[^i]}** rwlix,
/snap/{core/*/usr/lib/snapd/snap-confin[^e],snapd/*/usr/lib/snapd/snap-confi[^n]}** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-confin[^e]** rwlix,
`[1:],
		},
		{
			comment:         "anything except /lib/{firmware,modules} for non-core base template",
			excludePatterns: []string{"/lib/firmware", "/lib/modules"},
			suffix:          " mrklix,",
			expRule: `
/[^l]** mrklix,
/l[^i]** mrklix,
/li[^b]** mrklix,
/lib[^/]** mrklix,
/lib/[^fm]** mrklix,
/lib/{f[^i],m[^o]}** mrklix,
/lib/{fi[^r],mo[^d]}** mrklix,
/lib/{fir[^m],mod[^u]}** mrklix,
/lib/{firm[^w],modu[^l]}** mrklix,
/lib/{firmw[^a],modul[^e]}** mrklix,
/lib/{firmwa[^r],module[^s]}** mrklix,
/lib/firmwar[^e]** mrklix,
`[1:],
		},
		{
			comment:         "anything except /usr/src, /usr/lib/{firmware,snapd,modules} for non-core base template",
			excludePatterns: []string{"/usr/lib/firmware", "/usr/lib/modules", "/usr/lib/snapd"},
			suffix:          " mrklix,",
			expRule: `
/[^u]** mrklix,
/u[^s]** mrklix,
/us[^r]** mrklix,
/usr[^/]** mrklix,
/usr/[^l]** mrklix,
/usr/l[^i]** mrklix,
/usr/li[^b]** mrklix,
/usr/lib[^/]** mrklix,
/usr/lib/[^fms]** mrklix,
/usr/lib/{f[^i],m[^o],s[^n]}** mrklix,
/usr/lib/{fi[^r],mo[^d],sn[^a]}** mrklix,
/usr/lib/{fir[^m],mod[^u],sna[^p]}** mrklix,
/usr/lib/{firm[^w],modu[^l],snap[^d]}** mrklix,
/usr/lib/{firmw[^a],modul[^e]}** mrklix,
/usr/lib/{firmwa[^r],module[^s]}** mrklix,
/usr/lib/firmwar[^e]** mrklix,
`[1:],
		},
		{
			comment: "anything except /var/lib/{dhcp,extrausers,jenkins,snapd} /var/log /var/snap and /var/tmp for non-core base template",
			excludePatterns: []string{
				"/var/lib/dhcp",
				"/var/lib/extrausers",
				"/var/lib/jenkins",
				"/var/lib/snapd",
				"/var/log",
				"/var/snap",
				"/var/tmp",
			},
			suffix: " mrklix,",
			expRule: `
/[^v]** mrklix,
/v[^a]** mrklix,
/va[^r]** mrklix,
/var[^/]** mrklix,
/var/[^lst]** mrklix,
/var/{l[^io],s[^n],t[^m]}** mrklix,
/var/{li[^b],lo[^g],sn[^a],tm[^p]}** mrklix,
/var/{lib[^/],sna[^p]}** mrklix,
/var/lib/[^dejs]** mrklix,
/var/{lib/d[^h],lib/e[^x],lib/j[^e],lib/s[^n]}** mrklix,
/var/{lib/dh[^c],lib/ex[^t],lib/je[^n],lib/sn[^a]}** mrklix,
/var/{lib/dhc[^p],lib/ext[^r],lib/jen[^k],lib/sna[^p]}** mrklix,
/var/{lib/extr[^a],lib/jenk[^i],lib/snap[^d]}** mrklix,
/var/{lib/extra[^u],lib/jenki[^n]}** mrklix,
/var/{lib/extrau[^s],lib/jenkin[^s]}** mrklix,
/var/lib/extraus[^e]** mrklix,
/var/lib/extrause[^r]** mrklix,
/var/lib/extrauser[^s]** mrklix,
`[1:],
		},
		{
			comment: "everything except {/var/lib/snapd/hostfs,}/{dev,proc,sys} for system-backup",
			excludePatterns: []string{
				"/dev/",
				"/proc/",
				"/sys/",
				"/var/lib/snapd/hostfs/dev/",
				"/var/lib/snapd/hostfs/proc/",
				"/var/lib/snapd/hostfs/sys/",
			},
			expRule: `
/[^dpsv]**
/{d[^e],p[^r],s[^y],v[^a]}**
/{de[^v],pr[^o],sy[^s],va[^r]}**
/{dev[^/],pro[^c],sys[^/],var[^/]}**
/{proc[^/],var/[^l]}**
/var/l[^i]**
/var/li[^b]**
/var/lib[^/]**
/var/lib/[^s]**
/var/lib/s[^n]**
/var/lib/sn[^a]**
/var/lib/sna[^p]**
/var/lib/snap[^d]**
/var/lib/snapd[^/]**
/var/lib/snapd/[^h]**
/var/lib/snapd/h[^o]**
/var/lib/snapd/ho[^s]**
/var/lib/snapd/hos[^t]**
/var/lib/snapd/host[^f]**
/var/lib/snapd/hostf[^s]**
/var/lib/snapd/hostfs[^/]**
/var/lib/snapd/hostfs/[^dps]**
/{var/lib/snapd/hostfs/d[^e],var/lib/snapd/hostfs/p[^r],var/lib/snapd/hostfs/s[^y]}**
/{var/lib/snapd/hostfs/de[^v],var/lib/snapd/hostfs/pr[^o],var/lib/snapd/hostfs/sy[^s]}**
/{var/lib/snapd/hostfs/dev[^/],var/lib/snapd/hostfs/pro[^c],var/lib/snapd/hostfs/sys[^/]}**
/var/lib/snapd/hostfs/proc[^/]**
`[1:],
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		opts := &apparmor.AAREExclusionPatternsOptions{
			Prefix: t.prefix,
			Suffix: t.suffix,
		}
		fmt.Println(t.excludePatterns)
		res, err := apparmor.GenerateAAREExclusionPatterns(t.excludePatterns, opts)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			continue
		}
		c.Assert(err, IsNil)
		c.Assert(res, Equals, t.expRule, comment)
	}
}
