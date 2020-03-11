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
		env, err := osutil.ParseRawEnvironment(t.env)
		c.Assert(err, IsNil)
		c.Check(env, DeepEquals, osutil.Environment(t.expected), Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestParseRawEnvironmentNotKeyValue(c *C) {
	env, err := osutil.ParseRawEnvironment([]string{"KEY"})
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentEmptyKey(c *C) {
	env, err := osutil.ParseRawEnvironment([]string{"=VALUE"})
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDuplicateKey(c *C) {
	env, err := osutil.ParseRawEnvironment([]string{"K=1", "K=2"})
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestOSEnvironment(c *C) {
	env, err := osutil.OSEnvironment()
	c.Assert(err, IsNil)
	c.Check(len(os.Environ()), Equals, len(env.ForExec()))
	c.Check(os.Getenv("PATH"), Equals, env["PATH"])
}

func (s *envSuite) TestTransformRewriting(c *C) {
	env := osutil.Environment{"K": "V"}
	env.Transform(func(key, value string) (string, string) {
		return "key-" + key, "value-" + value
	})
	c.Assert(env.ForExec(), DeepEquals, []string{"key-K=value-V"})
}

func (s *envSuite) TestTransformSquashing(c *C) {
	env := osutil.Environment{"K": "1", "prefix-K": "2"}
	env.Transform(func(key, value string) (string, string) {
		key = strings.TrimPrefix(key, "prefix-")
		return key, value
	})
	c.Assert(env.ForExec(), DeepEquals, []string{"K=2"})
}

func (s *envSuite) TestTransformDeleting(c *C) {
	env := osutil.Environment{"K": "1", "prefix-K": "2"}
	env.Transform(func(key, value string) (string, string) {
		if strings.HasPrefix(key, "prefix-") {
			return "", ""
		}
		return key, value
	})
	c.Assert(env.ForExec(), DeepEquals, []string{"K=1"})
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
	eenv, err := osutil.ParseRawExpandableEnv([]string{"K1=V1", "K2=$K1"})
	c.Assert(err, IsNil)
	c.Check(eenv.Get("K1"), Equals, "V1")
	c.Check(eenv.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawExpandableEnvNotKeyValue(c *C) {
	eenv, err := osutil.ParseRawExpandableEnv([]string{"KEY"})
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(eenv, DeepEquals, osutil.ExpandableEnv{})
}

func (s *envSuite) TestParseRawExpandableEnvEmptyKey(c *C) {
	eenv, err := osutil.ParseRawExpandableEnv([]string{"=VALUE"})
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(eenv, DeepEquals, osutil.ExpandableEnv{})
}

func (s *envSuite) TestParseRawExpandableEnvDuplicateKey(c *C) {
	eenv, err := osutil.ParseRawExpandableEnv([]string{"K=1", "K=2"})
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(eenv, DeepEquals, osutil.ExpandableEnv{})
}

func (s *envSuite) TestSetExpandableEnv(c *C) {
	env := make(osutil.Environment)
	env["A"] = "a"
	undef := env.SetExpandableEnv(osutil.NewExpandableEnv(
		"B", "$C",
		"C", "$A",
		"D", "$D",
	))
	c.Check(undef, DeepEquals, []string{"D"})
	c.Check(env.ForExec(), DeepEquals, []string{"A=a", "B=a", "C=a", "D="})
}

func (s *envSuite) TestSetExpandableEnvForEnvOverride(c *C) {
	env := make(osutil.Environment)
	env["PATH"] = "system-value"
	undef := env.SetExpandableEnv(osutil.NewExpandableEnv(
		"PATH", "snap-level-override",
	))
	c.Check(undef, HasLen, 0)
	undef = env.SetExpandableEnv(osutil.NewExpandableEnv(
		"PATH", "app-level-override",
	))
	c.Check(undef, HasLen, 0)
	c.Check(env.ForExec(), DeepEquals, []string{"PATH=app-level-override"})
}

func (s *envSuite) TestSetExpandableEnvForEnvExpansion(c *C) {
	env := make(osutil.Environment)
	env["PATH"] = "system-value"
	undef := env.SetExpandableEnv(osutil.NewExpandableEnv(
		"PATH", "snap-ext:$PATH",
	))
	c.Check(undef, HasLen, 0)
	undef = env.SetExpandableEnv(osutil.NewExpandableEnv(
		"PATH", "app-ext:$PATH",
	))
	c.Check(undef, HasLen, 0)
	c.Check(env.ForExec(), DeepEquals, []string{"PATH=app-ext:snap-ext:system-value"})
}

func (s *envSuite) TestSetExpandableEnvVarious(c *C) {
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
		eenv, err := osutil.ParseRawExpandableEnv(strings.Split(t.env, ","))
		c.Assert(err, IsNil)
		env := make(osutil.Environment)
		if strings.Contains(t.env, "PATH") {
			env["PATH"] = os.Getenv("PATH")
		}
		env.SetExpandableEnv(eenv)
		delete(env, "PATH")
		c.Check(strings.Join(env.ForExec(), ","), DeepEquals, t.expected, Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}
