// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package osutil_test

import (
	"fmt"
	"math"
	"os"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type envSuite struct{}

var _ = Suite(&envSuite{})

func (s *envSuite) TestGetenvBoolTrue(c *C) {
	key := "__XYZZY__"
	os.Unsetenv(key)

	for _, s := range []string{
		"1", "t", "TRUE",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), Equals, s)
		c.Check(osutil.GetenvBool(key), Equals, true, Commentf(s))
		c.Check(osutil.GetenvBool(key, false), Equals, true, Commentf(s))
		c.Check(osutil.GetenvBool(key, true), Equals, true, Commentf(s))
	}
}

func (s *envSuite) TestGetenvBoolFalse(c *C) {
	key := "__XYZZY__"
	os.Unsetenv(key)
	c.Assert(osutil.GetenvBool(key), Equals, false)

	for _, s := range []string{
		"", "0", "f", "FALSE", "potato",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), Equals, s)
		c.Check(osutil.GetenvBool(key), Equals, false, Commentf(s))
		c.Check(osutil.GetenvBool(key, false), Equals, false, Commentf(s))
	}
}

func (s *envSuite) TestGetenvBoolFalseDefaultTrue(c *C) {
	key := "__XYZZY__"
	os.Unsetenv(key)
	c.Assert(osutil.GetenvBool(key), Equals, false)

	for _, s := range []string{
		"0", "f", "FALSE",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), Equals, s)
		c.Check(osutil.GetenvBool(key, true), Equals, false, Commentf(s))
	}

	for _, s := range []string{
		"", "potato", // etc
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), Equals, s)
		c.Check(osutil.GetenvBool(key, true), Equals, true, Commentf(s))
	}
}

func (s *envSuite) TestGetenvInt64(c *C) {
	key := "__XYZZY__"
	os.Unsetenv(key)

	c.Check(osutil.GetenvInt64(key), Equals, int64(0))
	c.Check(osutil.GetenvInt64(key, -1), Equals, int64(-1))
	c.Check(osutil.GetenvInt64(key, math.MaxInt64), Equals, int64(math.MaxInt64))
	c.Check(osutil.GetenvInt64(key, math.MinInt64), Equals, int64(math.MinInt64))

	for _, n := range []int64{
		0, -1, math.MinInt64, math.MaxInt64,
	} {
		for _, tpl := range []string{"%d", "  %d  ", "%#x", "%#X", "%#o"} {
			v := fmt.Sprintf(tpl, n)
			os.Setenv(key, v)
			c.Assert(os.Getenv(key), Equals, v)
			c.Check(osutil.GetenvInt64(key), Equals, n, Commentf(v))
		}
	}
}

func (s *envSuite) TestParseRawEnvironmentHappy(c *C) {
	for _, t := range []struct {
		env      []string
		expected map[string]string
	}{
		{
			[]string{"K=V"},
			map[string]string{"K": "V"},
		},
		{
			[]string{"K=V=V=V"},
			map[string]string{"K": "V=V=V"},
		},
		{
			[]string{"K1=V1", "K2=V2"},
			map[string]string{"K1": "V1", "K2": "V2"},
		},
	} {
		env := mylog.Check2(osutil.ParseRawEnvironment(t.env))

		c.Check(env, DeepEquals, osutil.Environment(t.expected), Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestParseRawEnvironmentNotKeyValue(c *C) {
	env := mylog.Check2(osutil.ParseRawEnvironment([]string{"KEY"}))
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentEmptyKey(c *C) {
	env := mylog.Check2(osutil.ParseRawEnvironment([]string{"=VALUE"}))
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDuplicateKey(c *C) {
	env := mylog.Check2(osutil.ParseRawEnvironment([]string{"K=1", "K=2"}))
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestOSEnvironment(c *C) {
	env := mylog.Check2(osutil.OSEnvironment())

	c.Check(len(os.Environ()), Equals, len(env.ForExec()))
	c.Check(os.Getenv("PATH"), Equals, env["PATH"])
}

func (s *envSuite) TestOSEnvironmentUnescapeUnsafe(c *C) {
	os.Setenv("SNAPD_UNSAFE_PREFIX_A", "a")
	defer os.Unsetenv("SNAPD_UNSAFE_PREFIX_A")
	os.Setenv("SNAPDEXTRA", "2")
	defer os.Unsetenv("SNAPDEXTRA")
	os.Setenv("SNAPD_UNSAFE_PREFIX_SNAPDEXTRA", "1")
	defer os.Unsetenv("SNAPD_UNSAFE_PREFIX_SNAPDEXTRA")

	env := mylog.Check2(osutil.OSEnvironmentUnescapeUnsafe("SNAPD_UNSAFE_PREFIX_"))

	// -1 because only the unescaped SNAPDEXTRA is kept
	c.Check(len(os.Environ())-1, Equals, len(env.ForExec()))
	c.Check(os.Getenv("PATH"), Equals, env["PATH"])
	c.Check("a", Equals, env["A"])
	c.Check("2", Equals, env["SNAPDEXTRA"])
}

func (s *envSuite) TestGet(c *C) {
	env := osutil.Environment{"K": "V"}
	c.Assert(env["K"], Equals, "V")
	c.Assert(env["missing"], Equals, "")
}

func (s *envSuite) TestDel(c *C) {
	env := osutil.Environment{"K": "V"}
	delete(env, "K")
	c.Assert(env["K"], Equals, "")
	delete(env, "missing")
	c.Assert(env["missing"], Equals, "")
}

func (s *envSuite) TestForExec(c *C) {
	env := osutil.Environment{"K1": "V1", "K2": "V2"}
	c.Check(env.ForExec(), DeepEquals, []string{"K1=V1", "K2=V2"})
}

func (s *envSuite) TestNewExpandableEnv(c *C) {
	eenv := osutil.NewExpandableEnv("K1", "V1", "K2", "$K1")
	c.Check(eenv.Get("K1"), Equals, "V1")
	c.Check(eenv.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawExpandableEnvHappy(c *C) {
	eenv := mylog.Check2(osutil.ParseRawExpandableEnv([]string{"K1=V1", "K2=$K1"}))

	c.Check(eenv.Get("K1"), Equals, "V1")
	c.Check(eenv.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawExpandableEnvNotKeyValue(c *C) {
	eenv := mylog.Check2(osutil.ParseRawExpandableEnv([]string{"KEY"}))
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(eenv, DeepEquals, osutil.ExpandableEnv{})
}

func (s *envSuite) TestParseRawExpandableEnvEmptyKey(c *C) {
	eenv := mylog.Check2(osutil.ParseRawExpandableEnv([]string{"=VALUE"}))
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(eenv, DeepEquals, osutil.ExpandableEnv{})
}

func (s *envSuite) TestParseRawExpandableEnvDuplicateKey(c *C) {
	eenv := mylog.Check2(osutil.ParseRawExpandableEnv([]string{"K=1", "K=2"}))
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(eenv, DeepEquals, osutil.ExpandableEnv{})
}

func (s *envSuite) TestExtendWithExpanded(c *C) {
	env := osutil.Environment{"A": "a"}
	env.ExtendWithExpanded(osutil.NewExpandableEnv(
		"B", "$C", // $C is undefined so it expands to ""
		"C", "$A", // $A is defined in the environment so it expands to "a"
		"D", "$D", // $D is undefined so it expands to ""
	))
	c.Check(env, DeepEquals, osutil.Environment{"A": "a", "B": "", "C": "a", "D": ""})
}

func (s *envSuite) TestExtendWithExpandedOfNil(c *C) {
	var env osutil.Environment
	env.ExtendWithExpanded(osutil.NewExpandableEnv(
		"A", "a",
		"B", "$C", // $C is undefined so it expands to ""
		"C", "$A", // $A is defined in the environment so it expands to "a"
		"D", "$D", // $D is undefined so it expands to ""
	))
	c.Check(env, DeepEquals, osutil.Environment{"A": "a", "B": "", "C": "a", "D": ""})
}

func (s *envSuite) TestExtendWithExpandedForEnvOverride(c *C) {
	env := osutil.Environment{"PATH": "system-value"}
	env.ExtendWithExpanded(osutil.NewExpandableEnv("PATH", "snap-level-override"))
	env.ExtendWithExpanded(osutil.NewExpandableEnv("PATH", "app-level-override"))
	c.Check(env, DeepEquals, osutil.Environment{"PATH": "app-level-override"})
}

func (s *envSuite) TestExtendWithExpandedForEnvExpansion(c *C) {
	env := osutil.Environment{"PATH": "system-value"}
	env.ExtendWithExpanded(osutil.NewExpandableEnv("PATH", "snap-ext:$PATH"))
	env.ExtendWithExpanded(osutil.NewExpandableEnv("PATH", "app-ext:$PATH"))
	c.Check(env, DeepEquals, osutil.Environment{"PATH": "app-ext:snap-ext:system-value"})
}

func (s *envSuite) TestExtendWithExpandedVarious(c *C) {
	for _, t := range []struct {
		env      string
		expected string
	}{
		// trivial
		{"K1=V1,K2=V2", "K1=V1,K2=V2"},
		// simple (order is preserved)
		{"K=V,K2=$K", "K=V,K2=V"},
		// simple from environment
		{"K=$PATH", fmt.Sprintf("K=%s", os.Getenv("PATH"))},
		// append to substitution from environment
		{"K=${PATH}:/foo", fmt.Sprintf("K=%s", os.Getenv("PATH")+":/foo")},
		// multi-level
		{"A=1,B=$A/2,C=$B/3,D=$C/4", "A=1,B=1/2,C=1/2/3,D=1/2/3/4"},
		// parsing is top down
		{"A=$A", "A="},
		{"A=$B,B=$A", "A=,B="},
		{"A=$B,B=$C,C=$A", "A=,B=,C="},
	} {
		eenv := mylog.Check2(osutil.ParseRawExpandableEnv(strings.Split(t.env, ",")))

		env := osutil.Environment{}
		if strings.Contains(t.env, "PATH") {
			env["PATH"] = os.Getenv("PATH")
		}
		env.ExtendWithExpanded(eenv)
		delete(env, "PATH")
		c.Check(strings.Join(env.ForExec(), ","), DeepEquals, t.expected, Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestForExecEscapeUnsafe(c *C) {
	env := osutil.Environment{
		"FOO":             "foo",
		"LD_PRELOAD":      "/opt/lib/libfunky.so",
		"SNAP_DATA":       "snap-data",
		"SNAP_SAVED_WHAT": "what", // will be dropped
		"SNAP_SAVED":      "snap-saved",
		"SNAP_S":          "snap-s",
		"XDG_STUFF":       "xdg-stuff", // will be prefixed
		"TMPDIR":          "/var/tmp",  // will be prefixed
	}
	raw := env.ForExecEscapeUnsafe("SNAP_SAVED_")
	c.Check(raw, DeepEquals, []string{
		"FOO=foo",
		"SNAP_DATA=snap-data",
		"SNAP_S=snap-s",
		"SNAP_SAVED=snap-saved",
		"SNAP_SAVED_LD_PRELOAD=/opt/lib/libfunky.so",
		"SNAP_SAVED_TMPDIR=/var/tmp",
		"XDG_STUFF=xdg-stuff",
	})
}

func (s *envSuite) TestForExecEscapeUnsafeNothingToEscape(c *C) {
	env := osutil.Environment{
		"FOO":             "foo",
		"SNAP_DATA":       "snap-data",
		"SNAP_SAVED_WHAT": "what",
		"SNAP_SAVED":      "snap-saved",
		"SNAP_S":          "snap-s",
		"XDG_STUFF":       "xdg-stuff",
	}
	raw := env.ForExecEscapeUnsafe("SNAP_SAVED_")
	c.Check(raw, DeepEquals, []string{
		"FOO=foo",
		"SNAP_DATA=snap-data",
		"SNAP_S=snap-s",
		"SNAP_SAVED=snap-saved",
		"XDG_STUFF=xdg-stuff",
	})
}
