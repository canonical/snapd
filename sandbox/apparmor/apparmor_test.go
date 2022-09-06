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
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

func TestApparmor(t *testing.T) {
	TestingT(t)
}

type apparmorSuite struct{}

var _ = Suite(&apparmorSuite{})

func (*apparmorSuite) TestAppArmorFindAppArmorParser(c *C) {
	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore := apparmor.MockParserSearchPath(mockParserCmd.BinDir())
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return false })
	defer restore()
	path, args, internal, err := apparmor.FindAppArmorParser()
	c.Check(path, Equals, mockParserCmd.Exe())
	c.Check(args, DeepEquals, []string{})
	c.Check(internal, Equals, false)
	c.Check(err, Equals, nil)
}

func (*apparmorSuite) TestAppArmorFindInternalAppArmorParser(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)
	d := filepath.Join(dirs.SnapMountDir, "/snapd/42", "/usr/lib/snapd")
	c.Assert(os.MkdirAll(d, 0755), IsNil)
	p := filepath.Join(d, "apparmor_parser")
	c.Assert(ioutil.WriteFile(p, nil, 0755), IsNil)
	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(d, "snapd"), nil
	})
	defer restore()
	restore = apparmor.MockSnapdAppArmorSupportsReexec(func() bool { return true })
	defer restore()

	path, args, internal, err := apparmor.FindAppArmorParser()
	c.Check(err, IsNil)
	c.Check(args, DeepEquals, []string{
		"--config-file", filepath.Join(d, "/apparmor/parser.conf"),
		"--base", filepath.Join(d, "/apparmor.d"),
		"--policy-features", filepath.Join(d, "/apparmor.d/abi/3.0"),
	})
	c.Check(internal, Equals, true)
	c.Check(path, Equals, p)
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
}

func (s *apparmorSuite) TestProbeAppArmorParserFeatures(c *C) {
	var features = []string{"unsafe", "include-if-exists", "qipcrtr-socket", "mqueue", "cap-bpf", "cap-audit-read"}
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
		err := ioutil.WriteFile(filepath.Join(d, "codes"), []byte(contents), 0755)
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
	c.Assert(ioutil.WriteFile(p, nil, 0755), IsNil)
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
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "mqueue", "qipcrtr-socket", "unsafe"})
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
	c.Check(features, DeepEquals, []string{"cap-audit-read", "cap-bpf", "include-if-exists", "mqueue", "qipcrtr-socket", "unsafe"})

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

func (s *apparmorSuite) TestSnapdAppArmorSupportsReexecImpl(c *C) {
	fakeroot := c.MkDir()
	dirs.SetRootDir(fakeroot)

	// with no info file should indicate it does not support reexec
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)

	d := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	c.Assert(os.MkdirAll(d, 0755), IsNil)
	infoFile := filepath.Join(d, "info")
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=foo"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=foo\nSNAPD_APPARMOR_REEXEC=0"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=foo\nSNAPD_APPARMOR_REEXEC=foo"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, false)
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=foo\nSNAPD_APPARMOR_REEXEC=1"), 0644), IsNil)
	c.Check(apparmor.SnapdAppArmorSupportsRexecImpl(), Equals, true)
}
