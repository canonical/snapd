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

package snap_test

import (
	"encoding/json"

	"gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap"
)

type epochSuite struct{}

var _ = check.Suite(&epochSuite{})

var (
	// some duplication here maybe
	epochZeroStar                 = `0\* is an invalid epoch`
	hugeEpochNumber               = `epoch numbers must be less than 2³², but got .*`
	badEpochNumber                = `epoch numbers must be base 10 with no zero padding, but got .*`
	badEpochList                  = "epoch read/write attributes must be lists of epoch numbers"
	emptyEpochList                = "epoch list cannot be explicitly empty"
	epochListNotIncreasing        = "epoch list must be a strictly increasing sequence"
	epochListJustRidiculouslyLong = "epoch list must not have more than 10 entries"
	noEpochIntersection           = "epoch read and write lists must have a non-empty intersection"
)

func (s epochSuite) TestBadEpochs(c *check.C) {
	type Tt struct {
		s string
		e string
		y int
	}

	tests := []Tt{
		{s: `"rubbish"`, e: badEpochNumber},                        // SISO
		{s: `0xA`, e: badEpochNumber, y: 1},                        // no hex
		{s: `"0xA"`, e: badEpochNumber},                            //
		{s: `001`, e: badEpochNumber, y: 1},                        // no octal, in fact no zero prefixes at all
		{s: `"001"`, e: badEpochNumber},                            //
		{s: `{"read": 5}`, e: badEpochList},                        // when split, must be list
		{s: `{"write": 5}`, e: badEpochList},                       //
		{s: `{"read": "5"}`, e: badEpochList},                      //
		{s: `{"write": "5"}`, e: badEpochList},                     //
		{s: `{"read": "1*"}`, e: badEpochList},                     // what
		{s: `{"read": [-1]}`, e: badEpochNumber},                   // negative not allowed
		{s: `{"write": [-1]}`, e: badEpochNumber},                  //
		{s: `{"read": ["-1"]}`, e: badEpochNumber},                 //
		{s: `{"write": ["-1"]}`, e: badEpochNumber},                //
		{s: `{"read": ["yes"]}`, e: badEpochNumber},                // must be numbers
		{s: `{"write": ["yes"]}`, e: badEpochNumber},               //
		{s: `{"read": ["Ⅰ","Ⅱ"]}`, e: badEpochNumber},              // not roman numerals you idiot
		{s: `{"read": [0xA]}`, e: badEpochNumber, y: 1},            //
		{s: `{"read": [010]}`, e: badEpochNumber, y: 1},            //
		{s: `{"read": [9999999999]}`, e: hugeEpochNumber},          // you done yet?
		{s: `"0*"`, e: epochZeroStar},                              // 0* means nothing
		{s: `"42**"`, e: badEpochNumber},                           // N** is dead
		{s: `{"read": []}`, e: emptyEpochList},                     // explicitly empty is bad
		{s: `{"write": []}`, e: emptyEpochList},                    //
		{s: `{"read": [1,2,4,3]}`, e: epochListNotIncreasing},      // must be ordered
		{s: `{"read": [1,2,2,3]}`, e: epochListNotIncreasing},      // must be strictly increasing
		{s: `{"write": [4,3,2,1]}`, e: epochListNotIncreasing},     // ...*increasing*
		{s: `{"read": [0], "write": [1]}`, e: noEpochIntersection}, // must have at least one in common
		{s: `{"read": [0,1,2,3,4,5,6,7,8,9,10],
 "write": [0,1,2,3,4,5,6,7,8,9,10]}`, e: epochListJustRidiculouslyLong}, // must have <10 elements
	}

	for _, test := range tests {
		var v snap.Epoch
		mylog.Check(yaml.Unmarshal([]byte(test.s), &v))
		c.Check(err, check.ErrorMatches, test.e, check.Commentf("YAML: %#q", test.s))

		if test.y == 1 {
			continue
		}
		mylog.Check(json.Unmarshal([]byte(test.s), &v))
		c.Check(err, check.ErrorMatches, test.e, check.Commentf("JSON: %#q", test.s))
	}
}

func (s epochSuite) TestGoodEpochs(c *check.C) {
	type Tt struct {
		s string
		e snap.Epoch
		y int
	}

	tests := []Tt{
		{s: `0`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, y: 1},
		{s: `""`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `"0"`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `"2*"`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `{"read": [2]}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `{"read": [1, 2]}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `{"write": [2]}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `{"write": [1, 2]}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{1, 2}}},
		{s: `{"read": [2,4,8], "write": [2,3,5]}`, e: snap.Epoch{Read: []uint32{2, 4, 8}, Write: []uint32{2, 3, 5}}},
	}

	for _, test := range tests {
		var v snap.Epoch
		mylog.Check(yaml.Unmarshal([]byte(test.s), &v))
		c.Check(err, check.IsNil, check.Commentf("YAML: %s", test.s))
		c.Check(v, check.DeepEquals, test.e)

		if test.y > 0 {
			continue
		}
		mylog.Check(json.Unmarshal([]byte(test.s), &v))
		c.Check(err, check.IsNil, check.Commentf("JSON: %s", test.s))
		c.Check(v, check.DeepEquals, test.e)
	}
}

func (s epochSuite) TestGoodEpochsInSnapYAML(c *check.C) {
	defer snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})()

	type Tt struct {
		s string
		e snap.Epoch
	}

	tests := []Tt{
		{s: ``, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `epoch: null`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `epoch: 0`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `epoch: "0"`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `epoch: {}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `epoch: "2*"`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `epoch: {"read": [2]}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `epoch: {"read": [1, 2]}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `epoch: {"write": [2]}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `epoch: {"write": [1, 2]}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{1, 2}}},
		{s: `epoch: {"read": [2,4,8], "write": [2,3,5]}`, e: snap.Epoch{Read: []uint32{2, 4, 8}, Write: []uint32{2, 3, 5}}},
	}

	for _, test := range tests {
		info := mylog.Check2(snap.InfoFromSnapYaml([]byte(test.s)))
		c.Check(err, check.IsNil, check.Commentf("YAML: %s", test.s))
		c.Check(info.Epoch, check.DeepEquals, test.e)
	}
}

func (s epochSuite) TestGoodEpochsInJSON(c *check.C) {
	type Tt struct {
		s string
		e snap.Epoch
	}

	type Tinfo struct {
		Epoch snap.Epoch `json:"epoch"`
	}

	tests := []Tt{
		// {} should give snap.Epoch{Read: []uint32{0}, Write: []uint32{0}} but needs an UnmarshalJSON on the parent
		{s: `{"epoch": null}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{"epoch": "0"}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{"epoch": {}}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{"epoch": "2*"}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `{"epoch": {"read": [0]}}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{"epoch": {"write": [0]}}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{"epoch": {"read": [2]}}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `{"epoch": {"read": [1, 2]}}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `{"epoch": {"write": [2]}}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `{"epoch": {"write": [1, 2]}}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{1, 2}}},
		{s: `{"epoch": {"read": [2,4,8], "write": [2,3,5]}}`, e: snap.Epoch{Read: []uint32{2, 4, 8}, Write: []uint32{2, 3, 5}}},
	}

	for _, test := range tests {
		var info Tinfo
		mylog.Check(json.Unmarshal([]byte(test.s), &info))
		c.Check(err, check.IsNil, check.Commentf("JSON: %s", test.s))
		c.Check(info.Epoch, check.DeepEquals, test.e, check.Commentf("JSON: %s", test.s))
	}
}

func (s *epochSuite) TestEpochValidate(c *check.C) {
	validEpochs := []snap.Epoch{
		{},
		{Read: []uint32{0}, Write: []uint32{0}},
		{Read: []uint32{0, 1}, Write: []uint32{1}},
		{Read: []uint32{1}, Write: []uint32{1}},
		{Read: []uint32{399, 400}, Write: []uint32{400}},
		{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}},
	}
	for _, epoch := range validEpochs {
		mylog.Check(epoch.Validate())
		c.Check(err, check.IsNil, check.Commentf("%s", epoch))
	}
	invalidEpochs := []struct {
		epoch snap.Epoch
		err   string
	}{
		{epoch: snap.Epoch{Read: []uint32{}}, err: emptyEpochList},
		{epoch: snap.Epoch{Write: []uint32{}}, err: emptyEpochList},
		{epoch: snap.Epoch{Read: []uint32{}, Write: []uint32{}}, err: emptyEpochList},
		{epoch: snap.Epoch{Read: []uint32{1}, Write: []uint32{2}}, err: noEpochIntersection},
		{epoch: snap.Epoch{Read: []uint32{1, 3, 5}, Write: []uint32{2, 4, 6}}, err: noEpochIntersection},
		{epoch: snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{3, 2, 1}}, err: epochListNotIncreasing},
		{epoch: snap.Epoch{Read: []uint32{3, 2, 1}, Write: []uint32{1, 2, 3}}, err: epochListNotIncreasing},
		{epoch: snap.Epoch{Read: []uint32{3, 2, 1}, Write: []uint32{3, 2, 1}}, err: epochListNotIncreasing},
		{epoch: snap.Epoch{Read: []uint32{0, 0, 0}, Write: []uint32{0}}, err: epochListNotIncreasing},
		{epoch: snap.Epoch{Read: []uint32{0}, Write: []uint32{0, 0, 0}}, err: epochListNotIncreasing},
		{epoch: snap.Epoch{Read: []uint32{0, 0, 0}, Write: []uint32{0, 0, 0}}, err: epochListNotIncreasing},
		{epoch: snap.Epoch{
			Read:  []uint32{0},
			Write: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		}, err: epochListJustRidiculouslyLong},
		{epoch: snap.Epoch{
			Read:  []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			Write: []uint32{0},
		}, err: epochListJustRidiculouslyLong},
		{epoch: snap.Epoch{
			Read:  []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			Write: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		}, err: epochListJustRidiculouslyLong},
	}
	for _, test := range invalidEpochs {
		mylog.Check(test.epoch.Validate())
		c.Check(err, check.ErrorMatches, test.err, check.Commentf("%s", test.epoch))
	}
}

func (s *epochSuite) TestEpochString(c *check.C) {
	tests := []struct {
		e snap.Epoch
		s string
	}{
		{e: snap.Epoch{}, s: "0"},
		{e: snap.Epoch{Read: []uint32{0}}, s: "0"},
		{e: snap.Epoch{Write: []uint32{0}}, s: "0"},
		{e: snap.Epoch{Read: []uint32{0}, Write: []uint32{}}, s: "0"},
		{e: snap.Epoch{Read: []uint32{}, Write: []uint32{0}}, s: "0"},
		{e: snap.Epoch{Read: []uint32{}, Write: []uint32{}}, s: "0"},
		{e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, s: "0"},
		{e: snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, s: "1*"},
		{e: snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, s: "1"},
		{e: snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}, s: "400*"},
		{e: snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}}, s: `{"read":[1,2,3],"write":[1,2,3]}`},
	}
	for _, test := range tests {
		c.Check(test.e.String(), check.Equals, test.s, check.Commentf(test.s))
	}
}

func (s *epochSuite) TestEpochMarshal(c *check.C) {
	tests := []struct {
		e snap.Epoch
		s string
	}{
		{e: snap.Epoch{}, s: `{"read":[0],"write":[0]}`},
		{e: snap.Epoch{Read: []uint32{0}}, s: `{"read":[0],"write":[0]}`},
		{e: snap.Epoch{Write: []uint32{0}}, s: `{"read":[0],"write":[0]}`},
		{e: snap.Epoch{Read: []uint32{0}, Write: []uint32{}}, s: `{"read":[0],"write":[0]}`},
		{e: snap.Epoch{Read: []uint32{}, Write: []uint32{0}}, s: `{"read":[0],"write":[0]}`},
		{e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, s: `{"read":[0],"write":[0]}`},
		{e: snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, s: `{"read":[0,1],"write":[1]}`},
		{e: snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, s: `{"read":[1],"write":[1]}`},
		{e: snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}, s: `{"read":[399,400],"write":[400]}`},
		{e: snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}}, s: `{"read":[1,2,3],"write":[1,2,3]}`},
	}
	for _, test := range tests {
		bs := mylog.Check2(test.e.MarshalJSON())
		c.Assert(err, check.IsNil)
		c.Check(string(bs), check.Equals, test.s, check.Commentf(test.s))
		bs = mylog.Check2(json.Marshal(test.e))
		c.Assert(err, check.IsNil)
		c.Check(string(bs), check.Equals, test.s, check.Commentf(test.s))
	}
}

func (s *epochSuite) TestE(c *check.C) {
	tests := []struct {
		e snap.Epoch
		s string
	}{
		{s: "0", e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: "1", e: snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}},
		{s: "1*", e: snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}},
		{s: "400*", e: snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}},
	}
	for _, test := range tests {
		c.Check(snap.E(test.s), check.DeepEquals, test.e, check.Commentf(test.s))
		c.Check(test.e.String(), check.Equals, test.s, check.Commentf(test.s))
	}
}

func (s *epochSuite) TestIsZero(c *check.C) {
	for _, e := range []*snap.Epoch{
		nil,
		{},
		{Read: []uint32{0}},
		{Write: []uint32{0}},
		{Read: []uint32{0}, Write: []uint32{}},
		{Read: []uint32{}, Write: []uint32{0}},
		{Read: []uint32{0}, Write: []uint32{0}},
	} {
		c.Check(e.IsZero(), check.Equals, true, check.Commentf("%#v", e))
	}
	for _, e := range []*snap.Epoch{
		{Read: []uint32{0, 1}, Write: []uint32{0}},
		{Read: []uint32{1}, Write: []uint32{1, 2}},
	} {
		c.Check(e.IsZero(), check.Equals, false, check.Commentf("%#v", e))
	}
}

func (s *epochSuite) TestCanRead(c *check.C) {
	tests := []struct {
		a, b   snap.Epoch
		ab, ba bool
	}{
		{ab: true, ba: true},                 // test for empty epoch
		{a: snap.E("0"), ab: true, ba: true}, // hybrid empty / zero
		{a: snap.E("0"), b: snap.E("1"), ab: false, ba: false},
		{a: snap.E("0"), b: snap.E("1*"), ab: false, ba: true},
		{a: snap.E("0"), b: snap.E("2*"), ab: false, ba: false},

		{
			a:  snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{2}},
			b:  snap.Epoch{Read: []uint32{1, 3, 4}, Write: []uint32{4}},
			ab: false,
			ba: false,
		},
		{
			a:  snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{3}},
			b:  snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{2}},
			ab: true,
			ba: true,
		},
	}
	for i, test := range tests {
		c.Assert(test.a.CanRead(test.b), check.Equals, test.ab, check.Commentf("ab/%d", i))
		c.Assert(test.b.CanRead(test.a), check.Equals, test.ba, check.Commentf("ba/%d", i))
	}
}

func (s *epochSuite) TestEqual(c *check.C) {
	tests := []struct {
		a, b *snap.Epoch
		eq   bool
	}{
		{a: &snap.Epoch{}, b: nil, eq: true},
		{a: &snap.Epoch{Read: []uint32{}, Write: []uint32{}}, b: nil, eq: true},
		{a: &snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, b: &snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, eq: true},
		{a: &snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, b: &snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, eq: true},
		{a: &snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, b: &snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, eq: false},
		{a: &snap.Epoch{Read: []uint32{1, 2, 3, 4}, Write: []uint32{7}}, b: &snap.Epoch{Read: []uint32{1, 2, 3, 7}, Write: []uint32{7}}, eq: false},
	}

	for i, test := range tests {
		c.Check(test.a.Equal(test.b), check.Equals, test.eq, check.Commentf("ab/%d", i))
		c.Check(test.b.Equal(test.a), check.Equals, test.eq, check.Commentf("ab/%d", i))
	}
}
