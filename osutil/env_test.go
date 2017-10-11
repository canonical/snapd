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

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type envSuite struct{}

var _ = check.Suite(&envSuite{})

func (s *envSuite) TestGetenvBoolTrue(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)

	for _, s := range []string{
		"1", "t", "TRUE",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key), check.Equals, true, check.Commentf(s))
		c.Check(osutil.GetenvBool(key, false), check.Equals, true, check.Commentf(s))
		c.Check(osutil.GetenvBool(key, true), check.Equals, true, check.Commentf(s))
	}
}

func (s *envSuite) TestGetenvBoolFalse(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)
	c.Assert(osutil.GetenvBool(key), check.Equals, false)

	for _, s := range []string{
		"", "0", "f", "FALSE", "potato",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key), check.Equals, false, check.Commentf(s))
		c.Check(osutil.GetenvBool(key, false), check.Equals, false, check.Commentf(s))
	}
}

func (s *envSuite) TestGetenvBoolFalseDefaultTrue(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)
	c.Assert(osutil.GetenvBool(key), check.Equals, false)

	for _, s := range []string{
		"0", "f", "FALSE",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key, true), check.Equals, false, check.Commentf(s))
	}

	for _, s := range []string{
		"", "potato", // etc
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key, true), check.Equals, true, check.Commentf(s))
	}
}

func (s *envSuite) TestGetenvInt64(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)

	c.Check(osutil.GetenvInt64(key), check.Equals, int64(0))
	c.Check(osutil.GetenvInt64(key, -1), check.Equals, int64(-1))
	c.Check(osutil.GetenvInt64(key, math.MaxInt64), check.Equals, int64(math.MaxInt64))
	c.Check(osutil.GetenvInt64(key, math.MinInt64), check.Equals, int64(math.MinInt64))

	for _, n := range []int64{
		0, -1, math.MinInt64, math.MaxInt64,
	} {
		for _, tpl := range []string{"%d", "  %d  ", "%#x", "%#X", "%#o"} {
			v := fmt.Sprintf(tpl, n)
			os.Setenv(key, v)
			c.Assert(os.Getenv(key), check.Equals, v)
			c.Check(osutil.GetenvInt64(key), check.Equals, n, check.Commentf(v))
		}
	}
}

func (s *envSuite) TestSubstitueEnv(c *check.C) {
	for _, t := range []struct {
		env string

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
		env := osutil.SubstituteEnv(strings.Split(t.env, ","))
		c.Check(strings.Join(env, ","), check.DeepEquals, t.expected, check.Commentf("invalid result for %q, got %q expected %q", t.env, env, t.expected))
	}
}

func (s *envSuite) TestEnvMap(c *check.C) {
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
		{
			// invalid input is handled gracefully
			[]string{"KEY_ONLY"},
			map[string]string{},
		},
	} {
		m := osutil.EnvMap(t.env)
		c.Check(m, check.DeepEquals, t.expected, check.Commentf("invalid result for %q, got %q expected %q", t.env, m, t.expected))
	}
}
