// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package strutil_test

import (
	"fmt"
	"os"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type envSuite struct{}

var _ = Suite(&envSuite{})

func (s *envSuite) TestNewEnvironment(c *C) {
	env := strutil.NewEnvironment(nil)
	c.Assert(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{})

	env = strutil.NewEnvironment(map[string]string{"K": "V", "K2": "V2"})
	c.Assert(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"K=V", "K2=V2"})
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
		env, err := strutil.ParseRawEnvironment(t.env)
		c.Assert(err, IsNil)
		c.Check(env, DeepEquals, strutil.NewEnvironment(t.expected), Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestParseRawEnvironmentNotKeyValue(c *C) {
	env, err := strutil.ParseRawEnvironment([]string{"KEY"})
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentEmptyKey(c *C) {
	env, err := strutil.ParseRawEnvironment([]string{"=VALUE"})
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDuplicateKey(c *C) {
	env, err := strutil.ParseRawEnvironment([]string{"K=1", "K=2"})
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(env, IsNil)
}

func (s *envSuite) TestOSEnvironment(c *C) {
	env, err := strutil.OSEnvironment()
	c.Assert(err, IsNil)
	c.Check(len(os.Environ()), Equals, len(env.RawEnvironment()))
	c.Check(os.Getenv("PATH"), Equals, env.Get("PATH"))
}

func (s *envSuite) TestTransformRewriting(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K": "V"})
	env.Transform(func(key, value string) (string, string) {
		return "key-" + key, "value-" + value
	})
	c.Assert(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"key-K=value-V"})
}

func (s *envSuite) TestTransformSquashing(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K": "1", "prefix-K": "2"})
	env.Transform(func(key, value string) (string, string) {
		key = strings.TrimPrefix(key, "prefix-")
		return key, value
	})
	c.Assert(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"K=2"})
}

func (s *envSuite) TestTransformDeleting(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K": "1", "prefix-K": "2"})
	env.Transform(func(key, value string) (string, string) {
		if strings.HasPrefix(key, "prefix-") {
			return "", ""
		}
		return key, value
	})
	c.Assert(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"K=1"})
}

func (s *envSuite) TestGet(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K": "V"})
	c.Assert(env.Get("K"), Equals, "V")
	c.Assert(env.Get("missing"), Equals, "")
}

func (s *envSuite) TestContains(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K": "V"})
	c.Assert(env.Contains("K"), Equals, true)
	c.Assert(env.Contains("missing"), Equals, false)
}

func (s *envSuite) TestDel(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K": "V"})
	env.Del("K")
	c.Assert(env.Get("K"), Equals, "")
	c.Assert(env.Contains("K"), Equals, false)
	env.Del("missing")
	c.Assert(env.Get("missing"), Equals, "")
	c.Assert(env.Contains("missing"), Equals, false)
}

func (s *envSuite) TestSet(c *C) {
	var env strutil.Environment
	env.Set("K", "1")
	c.Assert(env.Get("K"), Equals, "1")
	env.Set("K", "2")
	c.Assert(env.Get("K"), Equals, "2")
}

func (s *envSuite) TestRawEnvironment(c *C) {
	env := strutil.NewEnvironment(map[string]string{"K1": "V1", "K2": "V2"})
	c.Check(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"K1=V1", "K2=V2"})
}
func (s *envSuite) TestNewEnvironmentDelta(c *C) {
	delta := strutil.NewEnvironmentDelta("K1", "V1", "K2", "$K1")
	c.Check(delta.Get("K1"), Equals, "V1")
	c.Check(delta.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawEnvironmentDeltaHappy(c *C) {
	delta, err := strutil.ParseRawEnvironmentDelta([]string{"K1=V1", "K2=$K1"})
	c.Assert(err, IsNil)
	c.Check(delta.Get("K1"), Equals, "V1")
	c.Check(delta.Get("K2"), Equals, "$K1")
}

func (s *envSuite) TestParseRawEnvironmentDeltaNotKeyValue(c *C) {
	delta, err := strutil.ParseRawEnvironmentDelta([]string{"KEY"})
	c.Assert(err, ErrorMatches, `cannot parse environment entry: "KEY"`)
	c.Assert(delta, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDeltaEmptyKey(c *C) {
	delta, err := strutil.ParseRawEnvironmentDelta([]string{"=VALUE"})
	c.Assert(err, ErrorMatches, `environment variable name cannot be empty: "=VALUE"`)
	c.Assert(delta, IsNil)
}

func (s *envSuite) TestParseRawEnvironmentDeltaDuplicateKey(c *C) {
	delta, err := strutil.ParseRawEnvironmentDelta([]string{"K=1", "K=2"})
	c.Assert(err, ErrorMatches, `cannot overwrite earlier value of "K"`)
	c.Assert(delta, IsNil)
}

func (s *envSuite) TestCopyDelta(c *C) {
	d1 := strutil.NewEnvironmentDelta("K1", "V1", "K2", "$K1")
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
	d1 := strutil.NewEnvironmentDelta("K1", "V1-old", "K2", "$K1")
	d2 := strutil.NewEnvironmentDelta("K1", "V1-new", "K3", "V3")
	d1.Merge(d2)
	c.Check(d1, DeepEquals, strutil.NewEnvironmentDelta("K2", "$K1", "K1", "V1-new", "K3", "V3"))
}

func (s *envSuite) TestApplyDelta(c *C) {
	var env strutil.Environment
	env.Set("A", "a")
	undef := env.ApplyDelta(strutil.NewEnvironmentDelta(
		"B", "$C",
		"C", "$A",
		"D", "$D",
	))
	c.Check(undef, DeepEquals, []string{"D"})
	c.Check(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"A=a", "B=a", "C=a", "D="})
}

func (s *envSuite) TestApplyDeltaForEnvOverride(c *C) {
	var env strutil.Environment
	env.Set("PATH", "system-value")
	undef := env.ApplyDelta(strutil.NewEnvironmentDelta(
		"PATH", "snap-level-override",
	))
	c.Check(undef, HasLen, 0)
	undef = env.ApplyDelta(strutil.NewEnvironmentDelta(
		"PATH", "app-level-override",
	))
	c.Check(undef, HasLen, 0)
	c.Check(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"PATH=app-level-override"})
}

func (s *envSuite) TestApplyDeltaForEnvExpansion(c *C) {
	var env strutil.Environment
	env.Set("PATH", "system-value")
	undef := env.ApplyDelta(strutil.NewEnvironmentDelta(
		"PATH", "snap-ext:$PATH",
	))
	c.Check(undef, HasLen, 0)
	undef = env.ApplyDelta(strutil.NewEnvironmentDelta(
		"PATH", "app-ext:$PATH",
	))
	c.Check(undef, HasLen, 0)
	c.Check(env.RawEnvironment(), DeepEquals, strutil.RawEnvironment{"PATH=app-ext:snap-ext:system-value"})
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
		delta, err := strutil.ParseRawEnvironmentDelta(strings.Split(t.env, ","))
		c.Assert(err, IsNil)
		var env strutil.Environment
		if strings.Contains(t.env, "PATH") {
			env.Set("PATH", os.Getenv("PATH"))
		}
		env.ApplyDelta(delta)
		env.Del("PATH")
		c.Check(strings.Join(env.RawEnvironment(), ","), DeepEquals, t.expected, Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestRawEnvironmentstring(c *C) {
	raw := strutil.RawEnvironment{"K=V", "K2=V2"}
	c.Check(raw.String(), Equals, `"K=V", "K2=V2"`)
}
