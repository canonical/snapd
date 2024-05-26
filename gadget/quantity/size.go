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

package quantity

import (
	"errors"
	"fmt"
	"math"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

// Size describes the size in bytes.
type Size uint64

const (
	// SizeKiB is the byte size of one kibibyte (2^10 = 1024 bytes)
	SizeKiB = Size(1 << 10)
	// SizeMiB is the size of one mebibyte (2^20)
	SizeMiB = Size(1 << 20)
	// SizeGiB is the size of one gibibyte (2^30)
	SizeGiB = Size(1 << 30)
)

func (s *Size) String() string {
	if s == nil {
		return "unspecified"
	}
	return fmt.Sprintf("%d", *s)
}

// iecSizeString formats the size using multiples from IEC units (i.e.
// kibibytes, mebibytes), that is as multiples of 1024. Printed values are
// truncated to 2 decimal points.
func iecSizeString(sz int64) string {
	maxFloat := float64(1023.5)
	r := float64(sz)
	unit := "B"
	for _, rangeUnit := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		if r < maxFloat {
			break
		}
		r /= 1024
		unit = rangeUnit
	}
	precision := 0
	if math.Floor(r) != r {
		precision = 2
	}
	return fmt.Sprintf("%.*f %s", precision, r, unit)
}

// IECString formats the size using multiples from IEC units (i.e. kibibytes,
// mebibytes), that is as multiples of 1024. Printed values are truncated to 2
// decimal points.
func (s Size) IECString() string {
	return iecSizeString(int64(s))
}

func (s *Size) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var gs string
	mylog.Check(unmarshal(&gs))

	*s = mylog.Check2(ParseSize(gs))

	return err
}

// parseSizeOrOffset parses a string expressing size or offset in a gadget
// specific format.
func parseSizeOrOffset(szOrOffs string) (int64, error) {
	number, unit := mylog.Check3(strutil.SplitUnit(szOrOffs))

	switch unit {
	case "M":
		// MiB
		number = number * int64(SizeMiB)
	case "G":
		// GiB
		number = number * int64(SizeGiB)
	case "":
		// straight bytes

	default:
		return 0, fmt.Errorf("invalid suffix %q", unit)
	}
	return number, nil
}

// ParseSize parses a string expressing size in a gadget specific format. The
// accepted format is one of: <bytes> | <bytes/2^20>M | <bytes/2^30>G.
func ParseSize(gs string) (Size, error) {
	sz := mylog.Check2(parseSizeOrOffset(gs))
	if sz < 0 {
		return 0, errors.New("size cannot be negative")
	}
	return Size(sz), err
}
