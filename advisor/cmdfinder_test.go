// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package advisor_test

import (
	"os"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type cmdfinderSuite struct{}

var _ = Suite(&cmdfinderSuite{})

func (s *cmdfinderSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapCacheDir, 0755), IsNil)

	db, err := advisor.Create()
	c.Assert(err, IsNil)
	c.Assert(db.AddSnap("foo", "1.0", "foo summary", []string{"foo", "meh"}), IsNil)
	c.Assert(db.AddSnap("bar", "2.0", "bar summary", []string{"bar", "meh"}), IsNil)
	c.Assert(db.Commit(), IsNil)
}

func (s *cmdfinderSuite) TestFindSimilarWordsCnf(c *C) {
	words := advisor.SimilarWords("123")
	sort.Strings(words)
	c.Check(words, DeepEquals, []string{
		// calculated using CommandNotFound.py:similar_words("123")
		"-123", "-23", "0123", "023", "1-23", "1-3", "1023",
		"103", "1123", "113", "12", "12-", "12-3", "120",
		"1203", "121", "1213", "122", "1223", "123", "1233",
		"124", "1243", "125", "1253", "126", "1263", "127",
		"1273", "128", "1283", "129", "1293", "12_", "12_3",
		"12a", "12a3", "12b", "12b3", "12c", "12c3", "12d",
		"12d3", "12e", "12e3", "12f", "12f3", "12g", "12g3",
		"12h", "12h3", "12i", "12i3", "12j", "12j3", "12k",
		"12k3", "12l", "12l3", "12m", "12m3", "12n", "12n3",
		"12o", "12o3", "12p", "12p3", "12q", "12q3", "12r",
		"12r3", "12s", "12s3", "12t", "12t3", "12u", "12u3",
		"12v", "12v3", "12w", "12w3", "12x", "12x3", "12y",
		"12y3", "12z", "12z3", "13", "132", "1323", "133",
		"1423", "143", "1523", "153", "1623", "163", "1723",
		"173", "1823", "183", "1923", "193", "1_23", "1_3",
		"1a23", "1a3", "1b23", "1b3", "1c23", "1c3", "1d23",
		"1d3", "1e23", "1e3", "1f23", "1f3", "1g23", "1g3",
		"1h23", "1h3", "1i23", "1i3", "1j23", "1j3", "1k23",
		"1k3", "1l23", "1l3", "1m23", "1m3", "1n23", "1n3",
		"1o23", "1o3", "1p23", "1p3", "1q23", "1q3", "1r23",
		"1r3", "1s23", "1s3", "1t23", "1t3", "1u23", "1u3",
		"1v23", "1v3", "1w23", "1w3", "1x23", "1x3", "1y23",
		"1y3", "1z23", "1z3", "2123", "213", "223", "23",
		"3123", "323", "4123", "423", "5123", "523", "6123",
		"623", "7123", "723", "8123", "823", "9123", "923",
		"_123", "_23", "a123", "a23", "b123", "b23", "c123",
		"c23", "d123", "d23", "e123", "e23", "f123", "f23",
		"g123", "g23", "h123", "h23", "i123", "i23", "j123",
		"j23", "k123", "k23", "l123", "l23", "m123", "m23",
		"n123", "n23", "o123", "o23", "p123", "p23", "q123",
		"q23", "r123", "r23", "s123", "s23", "t123", "t23",
		"u123", "u23", "v123", "v23", "w123", "w23", "x123",
		"x23", "y123", "y23", "z123", "z23",
	})
}

func (s *cmdfinderSuite) TestFindSimilarWordsTrivial(c *C) {
	words := advisor.SimilarWords("hella")
	c.Check(words, testutil.Contains, "hello")
}

func (s *cmdfinderSuite) TestFindCommandHit(c *C) {
	cmds, err := advisor.FindCommand("meh")
	c.Assert(err, IsNil)
	c.Check(cmds, DeepEquals, []advisor.Command{
		{Snap: "foo", Version: "1.0", Command: "meh"},
		{Snap: "bar", Version: "2.0", Command: "meh"},
	})
}

func (s *cmdfinderSuite) TestFindCommandMiss(c *C) {
	cmds, err := advisor.FindCommand("moh")
	c.Assert(err, IsNil)
	c.Check(cmds, HasLen, 0)
}

func (s *cmdfinderSuite) TestFindMisspelledCommandHit(c *C) {
	cmds, err := advisor.FindMisspelledCommand("moh")
	c.Assert(err, IsNil)
	c.Check(cmds, DeepEquals, []advisor.Command{
		{Snap: "foo", Version: "1.0", Command: "meh"},
		{Snap: "bar", Version: "2.0", Command: "meh"},
	})
}

func (s *cmdfinderSuite) TestFindMisspelledCommandMiss(c *C) {
	cmds, err := advisor.FindMisspelledCommand("hello")
	c.Assert(err, IsNil)
	c.Check(cmds, HasLen, 0)
}

func (s *cmdfinderSuite) TestDumpCommands(c *C) {
	cmds, err := advisor.DumpCommands()
	c.Assert(err, IsNil)
	c.Check(cmds, DeepEquals, map[string]string{
		"foo": `[{"snap":"foo","version":"1.0"}]`,
		"bar": `[{"snap":"bar","version":"2.0"}]`,
		"meh": `[{"snap":"foo","version":"1.0"},{"snap":"bar","version":"2.0"}]`,
	})
}

func (s *cmdfinderSuite) TestFindMissingCommandsDB(c *C) {
	err := os.Remove(dirs.SnapCommandsDB)
	c.Assert(err, IsNil)

	cmds, err := advisor.FindMisspelledCommand("hello")
	c.Assert(err, IsNil)
	c.Check(cmds, HasLen, 0)

	cmds, err = advisor.FindCommand("hello")
	c.Assert(err, IsNil)
	c.Check(cmds, HasLen, 0)

	pkg, err := advisor.FindPackage("hello")
	c.Assert(err, IsNil)
	c.Check(pkg, IsNil)
}
