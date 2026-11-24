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
	"fmt"
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
			formats := snapstate.RefreshDeltaFormats(s.tr)
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
		{"processor : 0\nCPU implementer : 0x41\nCPU part : 0xd0b\n", true, ""},
		{"processor : 0\nCPU Part : 0xD0B\nCPU Implementer : 0X41\n\nprocessor : 1\nCPU implementer : 0x41\nCPU part : 0xd0c", true, ""},
		{"processor : 0\nCPU implementer : 0x41\nCPU part : 0xd0b\n\nprocessor : 1\nCPU part : 0xd03\nCPU implementer : 0x41\n", false, ""},
		{"processor : 0\nCPU implementer : 0x41\nCPU part : 0xd0b\n \t\nprocessor : 1\n", false, "missing CPU part"},
		{"processor : 0\nCPU part : 0xd01\nCPU implementer : 0x48\n", true, ""},
		{"processor : 0\nCPU implementer : 0x48\nCPU part : 0xd01\n\nprocessor : 1\nCPU implementer : 0x41\nCPU part : 0xd01\n", false, ""},
		{"processor : 0\nCPU implementer : 0x41\nCPU part : 0xd0b\n\nprocessor : 1\nCPU implementer : 0x48\nCPU part : 0xd01\n", true, ""},
		{"processor : 0\nCPU part : 0xd0b\n", false, "missing CPU implementer"},
		{"processor : 0\nCPU implementer : 0x41\nCPU part : 0xd0b\n\nprocessor : 1\nCPU part : 0xd0c\n", false, "missing CPU implementer"},
		{"CPU implementer : 0x41\n\nCPU part : 0xd0b\n", false, "missing CPU part"},
		{"CPU implementer : invalid\nCPU part : 0xd0b\n", false, "invalid CPU implementer"},
		{"CPU implementer : 0x141\nCPU part : 0xd0b\n", false, "invalid CPU implementer"},
		{"CPU implementer : 0xgg\nCPU part : 0xd0b\n", false, "invalid CPU implementer"},
		{"processor : 0\nCPU part : invalid\n", false, "invalid CPU part"},
		{"CPU part : 123\n", false, "invalid CPU part"},
		{"CPU part : 0xzzz\n", false, "invalid CPU part"},
		{"Hardware : unknown\n", false, "no CPU parts found"},
	} {
		c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cpuinfo"), []byte(tc.cpuinfo), 0644), IsNil)
		log, restore := logger.MockLogger()
		formats := snapstate.RefreshDeltaFormats(s.tr)
		restore()
		expected := []string{"xdelta3"}
		if tc.allowed {
			expected = []string{"snap-1-1-xdelta3", "xdelta3"}
		}
		c.Check(formats, DeepEquals, expected, Commentf("%s", tc.cpuinfo))
		if tc.log != "" {
			c.Check(log.String(), testutil.Contains, tc.log)
		}
	}
	for _, part := range []string{"0xc05", "0xc07", "0xc08", "0xd01", "0xd02", "0xd03", "0xd04", "0xd05", "0xd46", "0xd80", "0xd8f"} {
		for _, implementer := range []string{"0x41", "0x48"} {
			cpuinfo := fmt.Sprintf("CPU implementer : %s\nCPU part : %s\n", implementer, part)
			c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cpuinfo"), []byte(cpuinfo), 0644), IsNil)
			formats := snapstate.RefreshDeltaFormats(s.tr)
			expected := []string{"xdelta3"}
			if implementer != "0x41" {
				expected = []string{"snap-1-1-xdelta3", "xdelta3"}
			}
			c.Check(formats, DeepEquals, expected, Commentf("implementer=%s part=%s", implementer, part))
		}
	}
}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsARMUnreadable(c *C) {
	arch.SetArchitecture("arm64")
	log, restore := logger.MockLogger()
	defer restore()
	formats := snapstate.RefreshDeltaFormats(s.tr)
	c.Check(formats, DeepEquals, []string{"xdelta3"})
	c.Check(log.String(), testutil.Contains, "cannot determine ARM CPU suitability")

}

func (s *deltaFormatsSuite) TestValidateRefreshDeltaFormats(c *C) {
	for _, tc := range []struct {
		formats []string
		err     string
	}{
		{nil, "refresh.delta-formats must contain at least one delta format"},
		{[]string{}, "refresh.delta-formats must contain at least one delta format"},
		{[]string{"unknown"}, `unsupported delta format "unknown" in refresh.delta-formats`},
		{[]string{"xdelta3", "xdelta3"}, `duplicate delta format "xdelta3" in refresh.delta-formats`},
		{[]string{"xdelta3"}, ""},
		{[]string{"xdelta3", "snap-1-1-xdelta3"}, ""},
	} {
		err := snapstate.ValidateRefreshDeltaFormats(tc.formats)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsExplicitAndFallback(c *C) {
	arch.SetArchitecture("amd64")
	for _, tc := range []struct {
		value any
		want  []string
		log   string
	}{
		{nil, []string{"snap-1-1-xdelta3", "xdelta3"}, ""},
		{[]string{"xdelta3"}, []string{"xdelta3"}, ""},
		{[]string{"snap-1-1-xdelta3"}, []string{"snap-1-1-xdelta3"}, ""},
		{[]string{"xdelta3", "snap-1-1-xdelta3"}, []string{"xdelta3", "snap-1-1-xdelta3"}, ""},
		{[]string{"xdelta3", "bogus", "snap-1-1-xdelta3"}, []string{"xdelta3", "snap-1-1-xdelta3"}, "skipping unsupported delta format bogus"},
		{[]string{}, []string{"xdelta3"}, "contains no supported delta formats"},
		{[]string{"unknown"}, []string{"xdelta3"}, "contains no supported delta formats"},
		{[]any{nil}, []string{"xdelta3"}, "skipping unsupported delta format <nil>"},
		{[]any{"xdelta3", 1}, []string{"xdelta3"}, "skipping unsupported delta format 1"},
		{[]string{"xdelta3", "snap-1-1-xdelta3", "xdelta3"}, []string{"xdelta3", "snap-1-1-xdelta3"}, `skipping duplicate delta format "xdelta3"`},
		{"xdelta3", []string{"xdelta3"}, "must be a list of delta formats"},
		{true, []string{"xdelta3"}, "must be a list of delta formats"},
		{map[string]string{"format": "xdelta3"}, []string{"xdelta3"}, "must be a list of delta formats"},
	} {
		c.Assert(s.tr.Set("core", "refresh.delta-formats", tc.value), IsNil)
		log, restore := logger.MockLogger()
		formats := snapstate.RefreshDeltaFormats(s.tr)
		restore()
		c.Check(formats, DeepEquals, tc.want)
		if tc.log != "" {
			c.Check(log.String(), testutil.Contains, tc.log)
		} else {
			c.Check(log.String(), Equals, "")
		}
	}
}

func (s *deltaFormatsSuite) TestRefreshDeltaFormatsUnreadable(c *C) {
	arch.SetArchitecture("amd64")
	c.Assert(s.tr.Set("core", "refresh", true), IsNil)
	log, restore := logger.MockLogger()
	defer restore()
	c.Check(snapstate.RefreshDeltaFormats(s.tr), DeepEquals, []string{"xdelta3"})
	c.Check(log.String(), testutil.Contains, "cannot read refresh.delta-formats, using xdelta3")
}
