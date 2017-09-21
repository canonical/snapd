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
	"github.com/snapcore/snapd/snap"

	"gopkg.in/check.v1"
	"gopkg.in/yaml.v2"
)

type epochSuite struct{}

var _ = check.Suite(&epochSuite{})

func (s epochSuite) TestBadEpochs(c *check.C) {
	type Tt struct {
		s string
		e error
		y int
	}

	tests := []Tt{
		{s: `"rubbish"`, e: snap.ErrBadEpochNumber},                        // SISO
		{s: `0xA`, e: snap.ErrBadEpochNumber, y: 1},                        // no hex
		{s: `"0xA"`, e: snap.ErrBadEpochNumber},                            //
		{s: `001`, e: snap.ErrBadEpochNumber, y: 1},                        // no octal, in fact no zero prefixes at all
		{s: `"001"`, e: snap.ErrBadEpochNumber},                            //
		{s: `{"read": 5}`, e: snap.ErrBadEpochList},                        // when split, must be list
		{s: `{"write": 5}`, e: snap.ErrBadEpochList},                       //
		{s: `{"read": "5"}`, e: snap.ErrBadEpochList},                      //
		{s: `{"write": "5"}`, e: snap.ErrBadEpochList},                     //
		{s: `{"read": "1*"}`, e: snap.ErrBadEpochList},                     // what
		{s: `{"read": [-1]}`, e: snap.ErrBadEpochNumber},                   // negative not allowed
		{s: `{"write": [-1]}`, e: snap.ErrBadEpochNumber},                  //
		{s: `{"read": ["-1"]}`, e: snap.ErrBadEpochNumber},                 //
		{s: `{"write": ["-1"]}`, e: snap.ErrBadEpochNumber},                //
		{s: `{"read": ["yes"]}`, e: snap.ErrBadEpochNumber},                // must be numbers
		{s: `{"write": ["yes"]}`, e: snap.ErrBadEpochNumber},               //
		{s: `{"read": ["Ⅰ","Ⅱ"]}`, e: snap.ErrBadEpochNumber},              // not roman numerals you idiot
		{s: `{"read": [0xA]}`, e: snap.ErrBadEpochNumber, y: 1},            //
		{s: `{"read": [010]}`, e: snap.ErrBadEpochNumber, y: 1},            //
		{s: `{"read": [9999999999]}`, e: snap.ErrHugeEpochNumber},          // you done yet?
		{s: `"0*"`, e: snap.ErrEpochOhSplat},                               // 0* means nothing
		{s: `"42**"`, e: snap.ErrBadEpochNumber},                           // N** is dead
		{s: `{"read": []}`, e: snap.ErrEmptyEpochList},                     // explicitly empty is bad
		{s: `{"write": []}`, e: snap.ErrEmptyEpochList},                    //
		{s: `{"read": [1,2,4,3]}`, e: snap.ErrEpochListNotSorted},          // must be ordered
		{s: `{"write": [4,3,2,1]}`, e: snap.ErrEpochListNotSorted},         // ...in ascending order
		{s: `{"read": [0], "write": [1]}`, e: snap.ErrNoEpochIntersection}, // must have at least one in common
	}

	for _, test := range tests {
		var v snap.Epoch
		err := yaml.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.Equals, test.e, check.Commentf("YAML: %#q", test.s))

		if test.y == 1 {
			continue
		}
		err = json.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.Equals, test.e, check.Commentf("JSON: %#q", test.s))
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
		err := yaml.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.IsNil, check.Commentf("YAML: %s", test.s))
		c.Check(v, check.DeepEquals, test.e)

		if test.y > 0 {
			continue
		}

		err = json.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.IsNil, check.Commentf("JSON: %s", test.s))
		c.Check(v, check.DeepEquals, test.e)
	}
}

func (s *epochSuite) TestEpochZero(c *check.C) {
	z := snap.EpochZero()
	c.Check(z.String(), check.Equals, "0")
	c.Check(z, check.DeepEquals, snap.Epoch{
		Read:  []uint32{0},
		Write: []uint32{0},
	})
}

func (s *epochSuite) TestEpochValidate(c *check.C) {
	validEpochs := []snap.Epoch{
		snap.EpochZero(),
		{Read: []uint32{0}, Write: []uint32{0}}, // same as EpochZero()
		{Read: []uint32{0, 1}, Write: []uint32{1}},
		{Read: []uint32{1}, Write: []uint32{1}},
		{Read: []uint32{399, 400}, Write: []uint32{400}},
		{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}},
	}
	for _, epoch := range validEpochs {
		err := epoch.Validate()
		c.Check(err, check.IsNil, check.Commentf("%s", epoch))
	}
	invalidEpochs := []struct {
		epoch *snap.Epoch
		err   error
	}{
		{epoch: nil, err: snap.ErrEmptyEpochList},
		{epoch: &snap.Epoch{}, err: snap.ErrEmptyEpochList},
		{epoch: &snap.Epoch{Read: []uint32{1}, Write: []uint32{2}}, err: snap.ErrNoEpochIntersection},
		{epoch: &snap.Epoch{Read: []uint32{1, 3, 5}, Write: []uint32{2, 4, 6}}, err: snap.ErrNoEpochIntersection},
		{epoch: &snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{3, 2, 1}}, err: snap.ErrEpochListNotSorted},
		{epoch: &snap.Epoch{Read: []uint32{3, 2, 1}, Write: []uint32{1, 2, 3}}, err: snap.ErrEpochListNotSorted},
		{epoch: &snap.Epoch{Read: []uint32{3, 2, 1}, Write: []uint32{3, 2, 1}}, err: snap.ErrEpochListNotSorted},
	}
	for _, test := range invalidEpochs {
		err := test.epoch.Validate()
		c.Check(err, check.Equals, test.err, check.Commentf("%s", test.epoch))
	}
}

func (s *epochSuite) TestEpochString(c *check.C) {
	tests := []struct {
		e snap.Epoch
		s string
	}{
		{e: snap.EpochZero(), s: "0"},
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
		{e: snap.EpochZero(), s: `"0"`},
		{e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, s: `"0"`},
		{e: snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, s: `"1*"`},
		{e: snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, s: `"1"`},
		{e: snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}, s: `"400*"`},
		{e: snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}}, s: `{"read":[1,2,3],"write":[1,2,3]}`},
	}
	for _, test := range tests {
		bs, err := json.Marshal(test.e)
		c.Assert(err, check.IsNil)
		c.Check(string(bs), check.Equals, test.s, check.Commentf(test.s))
	}
}

func (s *epochSuite) TestE(c *check.C) {
	tests := []struct {
		e snap.Epoch
		s string
	}{
		{e: snap.EpochZero(), s: "0"},
		{e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, s: "0"},
		{e: snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, s: "1*"},
		{e: snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, s: "1"},
		{e: snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}, s: "400*"},
	}
	for _, test := range tests {
		c.Check(snap.E(test.s), check.DeepEquals, test.e, check.Commentf(test.s))
		c.Check(test.e.String(), check.Equals, test.s, check.Commentf(test.s))
	}
}
