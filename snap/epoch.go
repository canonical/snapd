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
	"errors"
	"fmt"
	"sort"
	"strconv"
)

var (
	ErrBadEpochExpression  = errors.New("invalid epoch expression")
	ErrEpochOhSplat        = errors.New("0* is an invalid epoch")
	ErrBadEpochNumber      = errors.New("epoch numbers must match [1-9][0-9]*")
	ErrHugeEpochNumber     = errors.New("epoch numbers must be less than 2³²")
	ErrBadEpochList        = errors.New("epoch read/write attributes must be lists of epoch numbers")
	ErrEmptyEpochList      = errors.New("epoch list cannot be explicitly empty")
	ErrEpochListNotSorted  = errors.New("epoch list must be in ascending order")
	ErrNoEpochIntersection = errors.New("epoch read and write lists must have a non-empty intersection")
)

// An Epoch represents the ability of the snap to read and write its data. Most
// developers need not worry about it, and snaps default to the 0th epoch, and
// users are only offered refreshes to epoch 0 snaps. Once an epoch bump is in
// order, there's a simplified expression they can use which should cover the
// majority of the cases:
//
//   epoch: N
//
// means a snap can read/write exactly the Nth epoch's data, and
//
//   epoch: N*
//
// means a snap can additionally read (N-1)th epoch's data, which means it's a
// snap that can migrate epochs (so a user on epoch 0 can get offered a refresh
// to a snap on epoch 1*).
//
// If the above is not enough, a developer can explicitly describe what epochs a
// snap can read and write:
//
//   epoch:
//     read: [1, 2, 3]
//     write: [1, 3]
//
// the read attribute defaults to the value of the write attribute, and the
// write attribute defaults to the last item in the read attribute. If both are
// unset, it's the same as not specifying an epoch at all (i.e. epoch: 0). The
// lists must be in ascending order, and there must be a non-empty intersection
// between them.
//
// Epoch numbers must be written as base 10 numbers, with no zero padding.
type Epoch struct {
	Read  []uint32 `yaml:"read"`
	Write []uint32 `yaml:"write"`
}

// E returns the epoch represented by the expression s. It's meant for use in
// testing, as it panics at the first sign of trouble.
func E(s string) Epoch {
	var e Epoch
	if err := e.fromEpochString(s); err != nil {
		panic(fmt.Errorf("%q: %v", s, err))
	}
	return e
}

// EpochZero returns the epoch represented by the expression "0".
func EpochZero() Epoch {
	return Epoch{Read: []uint32{0}, Write: []uint32{0}}
}

// IsNull checks whether this epoch looks like it's not been initialized.
func (e Epoch) IsNull() bool {
	return len(e.Read) == 0 && len(e.Write) == 0
}

func (e *Epoch) fromEpochString(s string) error {
	if len(s) == 0 {
		e.Read = []uint32{0}
		e.Write = []uint32{0}
		return nil
	}
	splat := false
	if s[len(s)-1] == '*' {
		splat = true
		s = s[:len(s)-1]
	}
	n, err := parseInt(s)
	if err != nil {
		return err
	}
	if splat {
		if n == 0 {
			return ErrEpochOhSplat
		}
		e.Read = []uint32{n - 1, n}
	} else {
		e.Read = []uint32{n}
	}
	e.Write = []uint32{n}

	return nil
}

func (e *Epoch) fromStructured(newEpoch structuredEpoch) error {
	if newEpoch.Read == nil {
		if newEpoch.Write == nil {
			newEpoch.Write = []uint32{0}
		}
		newEpoch.Read = newEpoch.Write
	} else if len(newEpoch.Read) == 0 {
		// this means they explicitly set it to []. Bad they!
		return ErrEmptyEpochList
	}
	if newEpoch.Write == nil {
		newEpoch.Write = newEpoch.Read[len(newEpoch.Read)-1:]
	} else if len(newEpoch.Write) == 0 {
		return ErrEmptyEpochList
	}
	e.Read = newEpoch.Read
	e.Write = newEpoch.Write

	if err := e.Validate(); err != nil {
		return err
	}

	return nil
}

func (e *Epoch) UnmarshalJSON(bs []byte) error {
	return e.UnmarshalYAML(func(v interface{}) error {
		return json.Unmarshal(bs, &v)
	})
}

func (e *Epoch) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var oldEpoch string
	if err := unmarshal(&oldEpoch); err == nil {
		return e.fromEpochString(oldEpoch)
	}
	var newEpoch structuredEpoch
	if err := unmarshal(&newEpoch); err != nil {
		return err
	}

	return e.fromStructured(newEpoch)
}

// Validate checks that the epoch makes sense.
func (e *Epoch) Validate() error {
	if e == nil || len(e.Read) == 0 || len(e.Write) == 0 {
		return ErrEmptyEpochList
	}
	if !(sort.IsSorted(uint32Slice(e.Read)) && sort.IsSorted(uint32Slice(e.Write))) {
		return ErrEpochListNotSorted
	}
	foundOne := false
	for i := range e.Read {
		idx := sort.Search(len(e.Write), func(j int) bool { return e.Write[j] >= e.Read[i] })
		if idx < len(e.Write) && e.Write[idx] == e.Read[i] {
			foundOne = true
			break
		}
	}
	if !foundOne {
		return ErrNoEpochIntersection
	}

	return nil
}

func (e *Epoch) simplify() interface{} {
	if len(e.Write) == 1 && len(e.Read) == 1 && e.Read[0] == e.Write[0] {
		return strconv.FormatUint(uint64(e.Read[0]), 10)
	}
	if len(e.Write) == 1 && len(e.Read) == 2 && e.Read[0]+1 == e.Read[1] && e.Read[1] == e.Write[0] {
		return strconv.FormatUint(uint64(e.Read[1]), 10) + "*"
	}
	return &structuredEpoch{Read: e.Read, Write: e.Write}
}

func (e Epoch) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.simplify())
}

func (e Epoch) String() string {
	i := e.simplify()
	if s, ok := i.(string); ok {
		return s
	}

	buf, err := json.Marshal(i)
	if err != nil {
		// can this happen?
		return fmt.Sprintf("%#v", e)
	}
	return string(buf)
}

type uint32Slice []uint32

func (ns uint32Slice) Len() int           { return len(ns) }
func (ns uint32Slice) Less(i, j int) bool { return ns[i] < ns[j] }
func (ns uint32Slice) Swap(i, j int)      { panic("no reordering") }

func parseInt(s string) (uint32, error) {
	if len(s) > 1 && s[0] == '0' {
		return 0, ErrBadEpochNumber
	}
	u, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		e, ok := err.(*strconv.NumError)
		if !ok {
			// strconv docs say this can't happen
			return 0, err
		}
		switch e.Err {
		case strconv.ErrSyntax:
			err = ErrBadEpochNumber
		case strconv.ErrRange:
			err = ErrHugeEpochNumber
		default:
			// strconv docs say this can't happen either
			err = e.Err
		}
	}
	return uint32(u), err
}

type zeroniner []uint32

func (z *zeroniner) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var ss []string
	if err := unmarshal(&ss); err != nil {
		return ErrBadEpochList
	}
	x := make([]uint32, len(ss))
	for i, s := range ss {
		n, err := parseInt(s)
		if err != nil {
			return err
		}
		x[i] = n
	}
	*z = x
	return nil
}

func (z *zeroniner) UnmarshalJSON(bs []byte) error {
	var ss []json.RawMessage
	if err := json.Unmarshal(bs, &ss); err != nil {
		return ErrBadEpochList
	}
	x := make([]uint32, len(ss))
	for i, s := range ss {
		n, err := parseInt(string(s))
		if err != nil {
			return err
		}
		x[i] = n
	}
	*z = x
	return nil
}

type structuredEpoch struct {
	Read  zeroniner `json:"read"`
	Write zeroniner `json:"write"`
}
