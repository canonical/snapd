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

package snap

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
)

// An Epoch represents the ability of the snap to read and write its data. Most
// developers need not worry about it, and snaps default to the 0th epoch, and
// users are only offered refreshes to epoch 0 snaps. Once an epoch bump is in
// order, there's a simplified expression they can use which should cover the
// majority of the cases:
//
//	epoch: N
//
// means a snap can read/write exactly the Nth epoch's data, and
//
//	epoch: N*
//
// means a snap can additionally read (N-1)th epoch's data, which means it's a
// snap that can migrate epochs (so a user on epoch 0 can get offered a refresh
// to a snap on epoch 1*).
//
// If the above is not enough, a developer can explicitly describe what epochs a
// snap can read and write:
//
//	epoch:
//	  read: [1, 2, 3]
//	  write: [1, 3]
//
// the read attribute defaults to the value of the write attribute, and the
// write attribute defaults to the last item in the read attribute. If both are
// unset, it's the same as not specifying an epoch at all (i.e. epoch: 0). The
// lists must not have more than 10 elements, they must be strictly increasing,
// and there must be a non-empty intersection between them.
//
// Epoch numbers must be written in base 10, with no zero padding.
type Epoch struct {
	Read  []uint32 `yaml:"read"`
	Write []uint32 `yaml:"write"`
}

// E returns the epoch represented by the expression s. It's meant for use in
// testing, as it panics at the first sign of trouble.
func E(s string) Epoch {
	var e Epoch
	mylog.Check(e.fromString(s))

	return e
}

func (e *Epoch) fromString(s string) error {
	if len(s) == 0 || s == "0" {
		e.Read = []uint32{0}
		e.Write = []uint32{0}
		return nil
	}
	star := false
	if s[len(s)-1] == '*' {
		star = true
		s = s[:len(s)-1]
	}
	n := mylog.Check2(parseInt(s))

	if star {
		if n == 0 {
			return &EpochError{Message: epochZeroStar}
		}
		e.Read = []uint32{n - 1, n}
	} else {
		e.Read = []uint32{n}
	}
	e.Write = []uint32{n}

	return nil
}

func (e *Epoch) fromStructured(structured structuredEpoch) error {
	if structured.Read == nil {
		if structured.Write == nil {
			structured.Write = []uint32{0}
		}
		structured.Read = structured.Write
	} else if len(structured.Read) == 0 {
		// this means they explicitly set it to []. Bad they!
		return &EpochError{Message: emptyEpochList}
	}
	if structured.Write == nil {
		structured.Write = structured.Read[len(structured.Read)-1:]
	} else if len(structured.Write) == 0 {
		return &EpochError{Message: emptyEpochList}
	}

	p := &Epoch{Read: structured.Read, Write: structured.Write}
	mylog.Check(p.Validate())

	*e = *p

	return nil
}

func (e *Epoch) UnmarshalJSON(bs []byte) error {
	return e.UnmarshalYAML(func(v interface{}) error {
		return json.Unmarshal(bs, &v)
	})
}

func (e *Epoch) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var shortEpoch string
	if mylog.Check(unmarshal(&shortEpoch)); err == nil {
		return e.fromString(shortEpoch)
	}
	var structured structuredEpoch
	mylog.Check(unmarshal(&structured))

	return e.fromStructured(structured)
}

// IsZero checks whether a snap's epoch is not set (or is set to the default
// value of "0").  Also zero are some epochs that would be normalized to "0",
// such as {"read": 0}, as well as some invalid ones like {"read": []}.
func (e *Epoch) IsZero() bool {
	if e == nil {
		return true
	}

	rZero := len(e.Read) == 0 || (len(e.Read) == 1 && e.Read[0] == 0)
	wZero := len(e.Write) == 0 || (len(e.Write) == 1 && e.Write[0] == 0)

	return rZero && wZero
}

func epochListEq(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (e *Epoch) Equal(other *Epoch) bool {
	if e.IsZero() {
		return other.IsZero()
	}
	return epochListEq(e.Read, other.Read) && epochListEq(e.Write, other.Write)
}

// Validate checks that the epoch makes sense.
func (e *Epoch) Validate() error {
	if (e.Read != nil && len(e.Read) == 0) || (e.Write != nil && len(e.Write) == 0) {
		// these are invalid, but if both are true then IsZero will be true.
		// In practice this check is redundant because it's caught in deserialise.
		// Belts-and-suspenders all the way down.
		return &EpochError{Message: emptyEpochList}
	}
	if e.IsZero() {
		return nil
	}
	if len(e.Read) > 10 || len(e.Write) > 10 {
		return &EpochError{Message: epochListJustRidiculouslyLong}
	}
	if !isIncreasing(e.Read) || !isIncreasing(e.Write) {
		return &EpochError{Message: epochListNotIncreasing}
	}

	if intersect(e.Read, e.Write) {
		return nil
	}
	return &EpochError{Message: noEpochIntersection}
}

func (e *Epoch) simplify() interface{} {
	if e.IsZero() {
		return "0"
	}
	if len(e.Write) == 1 && len(e.Read) == 1 && e.Read[0] == e.Write[0] {
		return strconv.FormatUint(uint64(e.Read[0]), 10)
	}
	if len(e.Write) == 1 && len(e.Read) == 2 && e.Read[0]+1 == e.Read[1] && e.Read[1] == e.Write[0] {
		return strconv.FormatUint(uint64(e.Read[1]), 10) + "*"
	}
	return &structuredEpoch{Read: e.Read, Write: e.Write}
}

func (e Epoch) MarshalJSON() ([]byte, error) {
	se := &structuredEpoch{Read: e.Read, Write: e.Write}
	if len(se.Read) == 0 {
		se.Read = uint32slice{0}
	}
	if len(se.Write) == 0 {
		se.Write = uint32slice{0}
	}
	return json.Marshal(se)
}

func (Epoch) MarshalYAML() (interface{}, error) {
	panic("unexpected attempt to marshal an Epoch to YAML")
}

func (e Epoch) String() string {
	i := e.simplify()
	if s, ok := i.(string); ok {
		return s
	}

	buf := mylog.Check2(json.Marshal(i))

	// can this happen?

	return string(buf)
}

// CanRead checks whether this epoch can read the data written by the
// other one.
func (e *Epoch) CanRead(other Epoch) bool {
	// the intersection between e.Read and other.Write needs to be non-empty

	// normalize (empty epoch should be treated like "0" here)
	var rs, ws []uint32
	if e != nil {
		rs = e.Read
	}
	ws = other.Write
	if len(rs) == 0 {
		rs = []uint32{0}
	}
	if len(ws) == 0 {
		ws = []uint32{0}
	}

	return intersect(rs, ws)
}

func intersect(rs, ws []uint32) bool {
	// O(𝑚𝑛) instead of O(𝑚log𝑛) for the binary search we could do, but
	// 𝑚 and 𝑛 < 10, so the simple solution is good enough (and if that
	// alone makes you nervous, know that it is ~2× faster in the worst
	// case; bisect starts being faster at ~50 entries).
	for _, r := range rs {
		for _, w := range ws {
			if r == w {
				return true
			}
		}
	}
	return false
}

// EpochError tracks the details of a failed epoch parse or validation.
type EpochError struct {
	Message string
}

func (e EpochError) Error() string {
	return e.Message
}

const (
	epochZeroStar                 = "0* is an invalid epoch"
	hugeEpochNumber               = "epoch numbers must be less than 2³², but got %q"
	badEpochNumber                = "epoch numbers must be base 10 with no zero padding, but got %q"
	badEpochList                  = "epoch read/write attributes must be lists of epoch numbers"
	emptyEpochList                = "epoch list cannot be explicitly empty"
	epochListNotIncreasing        = "epoch list must be a strictly increasing sequence"
	epochListJustRidiculouslyLong = "epoch list must not have more than 10 entries"
	noEpochIntersection           = "epoch read and write lists must have a non-empty intersection"
)

func parseInt(s string) (uint32, error) {
	if !(len(s) > 1 && s[0] == '0') {
		u := mylog.Check2(strconv.ParseUint(s, 10, 32))
		if err == nil {
			return uint32(u), nil
		}
		if e, ok := err.(*strconv.NumError); ok {
			if e.Err == strconv.ErrRange {
				return 0, &EpochError{
					Message: fmt.Sprintf(hugeEpochNumber, s),
				}
			}
		}
	}
	return 0, &EpochError{
		Message: fmt.Sprintf(badEpochNumber, s),
	}
}

type uint32slice []uint32

func (z *uint32slice) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var ss []string
	mylog.Check(unmarshal(&ss))

	x := make([]uint32, len(ss))
	for i, s := range ss {
		n := mylog.Check2(parseInt(s))

		x[i] = n
	}
	*z = x
	return nil
}

func (z *uint32slice) UnmarshalJSON(bs []byte) error {
	var ss []json.RawMessage
	mylog.Check(json.Unmarshal(bs, &ss))

	x := make([]uint32, len(ss))
	for i, s := range ss {
		n := mylog.Check2(parseInt(string(s)))

		x[i] = n
	}
	*z = x
	return nil
}

func isIncreasing(z []uint32) bool {
	if len(z) < 2 {
		return true
	}
	for i := range z[1:] {
		if z[i] >= z[i+1] {
			return false
		}
	}
	return true
}

type structuredEpoch struct {
	Read  uint32slice `json:"read"`
	Write uint32slice `json:"write"`
}
