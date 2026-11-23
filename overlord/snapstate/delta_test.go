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

package snapstate_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type deltaFormatsSuite struct {
	testutil.BaseTest
	tr *config.Transaction
}

var _ = Suite(&deltaFormatsSuite{})

func (s *deltaFormatsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	originalArch := arch.DpkgArchitecture()
	s.AddCleanup(func() { arch.SetArchitecture(arch.ArchitectureType(originalArch)) })
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	st := state.New(nil)
	st.Lock()
	s.AddCleanup(st.Unlock)
	s.tr = config.NewTransaction(st)
}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsDefaults(c *C) {
	for _, tc := range []struct {
		arch    string
		enabled bool
	}{
		{"amd64", true},
		{"ppc64el", true},
		{"armhf", false},
		{"i386", false},
		{"ppc64", false},
		{"riscv64", false},
		{"s390x", false},
	} {
		arch.SetArchitecture(arch.ArchitectureType(tc.arch))
		for _, flag := range []any{nil, false, true} {
			c.Assert(s.tr.Set("core", "experimental.snap-delta-format", flag), IsNil)
			formats, err := snapstate.RefreshDeltaFormats(s.tr)
			c.Assert(err, IsNil)
			expected := []string{"xdelta3"}
			if tc.enabled {
				expected = []string{"snap-1-1-xdelta3", "xdelta3"}
			}
			c.Check(formats, DeepEquals, expected, Commentf("arch=%s flag=%v", tc.arch, flag))
		}
	}
}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsARM(c *C) {
	arch.SetArchitecture("arm64")
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "proc"), 0755), IsNil)
	for _, tc := range []struct {
		cpuinfo string
		allowed bool
		log     string
	}{
		{"processor : 0\nCPU part : 0xd0b\n", true, ""},
		{"processor : 0\nCPU Part : 0xD0B\n\nprocessor : 1\nCPU part : 0xd0c", true, ""},
		{"processor : 0\nCPU part : 0xd0b\n\nprocessor : 1\nCPU part : 0xd03\n", false, ""},
		{"processor : 0\nCPU part : 0xd0b\n \t\nprocessor : 1\n", false, "missing CPU part"},
		{"processor : 0\nCPU part : invalid\n", false, "invalid CPU part"},
		{"CPU part : 123\n", false, "invalid CPU part"},
		{"CPU part : 0xzzz\n", false, "invalid CPU part"},
		{"Hardware : unknown\n", false, "no CPU parts found"},
	} {
		c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cpuinfo"), []byte(tc.cpuinfo), 0644), IsNil)
		log, restore := logger.MockLogger()
		formats, err := snapstate.RefreshDeltaFormats(s.tr)
		restore()
		c.Assert(err, IsNil)
		expected := []string{"xdelta3"}
		if tc.allowed {
			expected = []string{"snap-1-1-xdelta3", "xdelta3"}
		}
		c.Check(formats, DeepEquals, expected, Commentf("%s", tc.cpuinfo))
		if tc.log != "" {
			c.Check(log.String(), testutil.Contains, tc.log)
		}
	}
	for _, part := range []string{"0xc05", "0xc07", "0xc08", "0xd01", "0xd03", "0xd04", "0xd05", "0xd46", "0xd80"} {
		c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cpuinfo"), []byte("CPU part : "+part), 0644), IsNil)
		formats, err := snapstate.RefreshDeltaFormats(s.tr)
		c.Assert(err, IsNil)
		c.Check(formats, DeepEquals, []string{"xdelta3"}, Commentf("part=%s", part))
	}
}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsARMUnreadable(c *C) {
	arch.SetArchitecture("arm64")
	log, restore := logger.MockLogger()
	defer restore()
	formats, err := snapstate.RefreshDeltaFormats(s.tr)
	c.Assert(err, IsNil)
	c.Check(formats, DeepEquals, []string{"xdelta3"})
	c.Check(log.String(), testutil.Contains, "cannot determine ARM CPU suitability")

}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsExplicitAndInvalid(c *C) {
	arch.SetArchitecture("armhf")
	for _, tc := range []struct {
		value any
		want  []string
		err   string
	}{
		{[]string{"xdelta3"}, []string{"xdelta3"}, ""},
		{[]string{"snap-1-1-xdelta3"}, []string{"snap-1-1-xdelta3"}, ""},
		{[]string{"xdelta3", "snap-1-1-xdelta3"}, []string{"xdelta3", "snap-1-1-xdelta3"}, ""},
		{[]string{}, nil, "refresh.delta-formats must contain at least one delta format"},
		{[]string{"unknown"}, nil, `unsupported delta format "unknown" in refresh.delta-formats`},
		{[]any{nil}, nil, `unsupported delta format "" in refresh.delta-formats`},
		{[]any{"xdelta3", 1}, nil, ".*cannot unmarshal number.*"},
		{"xdelta3", nil, ".*cannot unmarshal string.*"},
		{true, nil, ".*cannot unmarshal bool.*"},
		{map[string]string{"format": "xdelta3"}, nil, ".*cannot unmarshal object.*"},
	} {
		c.Assert(s.tr.Set("core", "refresh.delta-formats", tc.value), IsNil)
		formats, err := snapstate.RefreshDeltaFormats(s.tr)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
			c.Check(formats, DeepEquals, tc.want)
		}
	}
}
