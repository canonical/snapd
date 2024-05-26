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

	"github.com/ddkwork/golibrary/mylog"
)

// Offset describes the offset in bytes and is a thin wrapper around Size.
type Offset Size

const (
	// OffsetKiB is the byte size of one kibibyte (2^10 = 1024 bytes)
	OffsetKiB = Offset(1 << 10)
	// OffsetMiB is the offset of one mebibyte (2^20)
	OffsetMiB = Offset(1 << 20)
)

func (o *Offset) String() string {
	return (*Size)(o).String()
}

// IECString formats the offset using multiples from IEC units (i.e. kibibytes,
// mebibytes), that is as multiples of 1024. Printed values are truncated to 2
// decimal points.
func (o *Offset) IECString() string {
	return iecSizeString(int64(*o))
}

func (o *Offset) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var gs string
	mylog.Check(unmarshal(&gs))

	*o = mylog.Check2(ParseOffset(gs))

	return err
}

// ParseOffset parses a string expressing offset in a gadget specific format. The
// accepted format is one of: <bytes> | <bytes/2^20>M | <bytes/2^30>G.
func ParseOffset(gs string) (Offset, error) {
	offs := mylog.Check2(parseSizeOrOffset(gs))
	if offs < 0 {
		// XXX: in theory offsets can be negative, but not in gadget
		// YAML
		return 0, errors.New("offset cannot be negative")
	}
	return Offset(offs), err
}
