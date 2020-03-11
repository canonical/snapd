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

func (s *envSuite) TestNewEnvironment(c *C) {
	env := osutil.NewEnvironment(nil)
	c.Assert(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{})

	env = osutil.NewEnvironment(map[string]string{"K": "V", "K2": "V2"})
	c.Assert(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"K=V", "K2=V2"})
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
		c.Check(env, DeepEquals, osutil.NewEnvironment(t.expected), Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
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
	c.Check(len(os.Environ()), Equals, len(env.RawEnvironment()))
	c.Check(os.Getenv("PATH"), Equals, env.Get("PATH"))
}

func (s *envSuite) TestTransformRewriting(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K": "V"})
	env.Transform(func(key, value string) (string, string) {
		return "key-" + key, "value-" + value
	})
	c.Assert(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"key-K=value-V"})
}

func (s *envSuite) TestTransformSquashing(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K": "1", "prefix-K": "2"})
	env.Transform(func(key, value string) (string, string) {
		key = strings.TrimPrefix(key, "prefix-")
		return key, value
	})
	c.Assert(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"K=2"})
}

func (s *envSuite) TestTransformDeleting(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K": "1", "prefix-K": "2"})
	env.Transform(func(key, value string) (string, string) {
		if strings.HasPrefix(key, "prefix-") {
			return "", ""
		}
		return key, value
	})
	c.Assert(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"K=1"})
}

func (s *envSuite) TestGet(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K": "V"})
	c.Assert(env.Get("K"), Equals, "V")
	c.Assert(env.Get("missing"), Equals, "")
}

func (s *envSuite) TestContains(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K": "V"})
	c.Assert(env.Contains("K"), Equals, true)
	c.Assert(env.Contains("missing"), Equals, false)
}

func (s *envSuite) TestDel(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K": "V"})
	env.Del("K")
	c.Assert(env.Get("K"), Equals, "")
	c.Assert(env.Contains("K"), Equals, false)
	env.Del("missing")
	c.Assert(env.Get("missing"), Equals, "")
	c.Assert(env.Contains("missing"), Equals, false)
}

func (s *envSuite) TestSet(c *C) {
	var env osutil.Environment
	env.Set("K", "1")
	c.Assert(env.Get("K"), Equals, "1")
	env.Set("K", "2")
	c.Assert(env.Get("K"), Equals, "2")
}

func (s *envSuite) TestRawEnvironment(c *C) {
	env := osutil.NewEnvironment(map[string]string{"K1": "V1", "K2": "V2"})
	c.Check(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"K1=V1", "K2=V2"})
}
func (s *envSuite) TestNewEnvironmentDelta(c *C) {
	delta := osutil.NewEnvironmentDelta("K1", "V1", "K2", "$K1")
	c.Check(delta.Get("K1"), Equals, "V1")
	c.Check(delta.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawEnvironmentDeltaHappy(c *C) {
	delta, err := osutil.ParseRawEnvironmentDelta([]string{"K1=V1", "K2=$K1"})
	c.Assert(err, IsNil)
	c.Check(delta.Get("K1"), Equals, "V1")
	c.Check(delta.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawEnvironmentDeltaNotKeyValue(c *C) {
	delta, err := osutil.ParseRawEnvironmentDelta([]string{"KEY"})
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(delta, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDeltaEmptyKey(c *C) {
	delta, err := osutil.ParseRawEnvironmentDelta([]string{"=VALUE"})
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(delta, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDeltaDuplicateKey(c *C) {
	delta, err := osutil.ParseRawEnvironmentDelta([]string{"K=1", "K=2"})
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(delta, IsNil)
}

func (s *envSuite) TestCopyDelta(c *C) {
	d1 := osutil.NewEnvironmentDelta("K1", "V1", "K2", "$K1")
	d2 := d1.Copy()
	c.Check(d2, DeepEquals, d1)

	d1.Set("K3", "foo")
	c.Check(d2.Get("K3"), Equals, "")

	d2.Set("K4", "bar")
	c.Check(d1.Get("K4"), Equals, "")

	c.Check(d1.Get("K3"), Equals, "foo")
	c.Check(d2.Get("K4"), Equals, "bar")
}

func (s *envSuite) TestMergeDeltas(c *C) {
	d1 := osutil.NewEnvironmentDelta("K1", "V1-old", "K2", "$K1")
	d2 := osutil.NewEnvironmentDelta("K1", "V1-new", "K3", "V3")
	d1.Merge(d2)
	c.Check(d1, DeepEquals, osutil.NewEnvironmentDelta("K2", "$K1", "K1", "V1-new", "K3", "V3"))
}

func (s *envSuite) TestApplyDelta(c *C) {
	var env osutil.Environment
	env.Set("A", "a")
	undef := env.ApplyDelta(osutil.NewEnvironmentDelta(
		"B", "$C",
		"C", "$A",
		"D", "$D",
	))
	c.Check(undef, DeepEquals, []string{"D"})
	c.Check(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"A=a", "B=a", "C=a", "D="})
}

func (s *envSuite) TestApplyDeltaForEnvOverride(c *C) {
	var env osutil.Environment
	env.Set("PATH", "system-value")
	undef := env.ApplyDelta(osutil.NewEnvironmentDelta(
		"PATH", "snap-level-override",
	))
	c.Check(undef, HasLen, 0)
	undef = env.ApplyDelta(osutil.NewEnvironmentDelta(
		"PATH", "app-level-override",
	))
	c.Check(undef, HasLen, 0)
	c.Check(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"PATH=app-level-override"})
}

func (s *envSuite) TestApplyDeltaForEnvExpansion(c *C) {
	var env osutil.Environment
	env.Set("PATH", "system-value")
	undef := env.ApplyDelta(osutil.NewEnvironmentDelta(
		"PATH", "snap-ext:$PATH",
	))
	c.Check(undef, HasLen, 0)
	undef = env.ApplyDelta(osutil.NewEnvironmentDelta(
		"PATH", "app-ext:$PATH",
	))
	c.Check(undef, HasLen, 0)
	c.Check(env.RawEnvironment(), DeepEquals, osutil.RawEnvironment{"PATH=app-ext:snap-ext:system-value"})
}

func (s *envSuite) TestApplyDeltaVarious(c *C) {
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
		delta, err := osutil.ParseRawEnvironmentDelta(strings.Split(t.env, ","))
		c.Assert(err, IsNil)
		var env osutil.Environment
		if strings.Contains(t.env, "PATH") {
			env.Set("PATH", os.Getenv("PATH"))
		}
		env.ApplyDelta(delta)
		env.Del("PATH")
		c.Check(strings.Join(env.RawEnvironment(), ","), DeepEquals, t.expected, Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestRawEnvironmentstring(c *C) {
	raw := osutil.RawEnvironment{"K=V", "K2=V2"}
	c.Check(raw.String(), Equals, `"K=V", "K2=V2"`)
}
