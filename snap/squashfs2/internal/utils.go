// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
 * https://www.kernel.org/doc/html/v5.8/filesystems/squashfs.html
 */

package internal

import "fmt"

type ParseError struct {
	Stype string
	Err   error
}

func (r *ParseError) Error() string {
	return fmt.Sprintf("squashfs: failed to parse %s: reason %v", r.Stype, r.Err)
}

func ReadUint16(data []byte) uint16 {
	return uint16(data[0]) | uint16(data[1])<<8
}

func ReadInt16(data []byte) int16 {
	return int16(ReadUint16(data))
}

func ReadUint32(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func ReadInt32(data []byte) int32 {
	return int32(ReadUint32(data))
}

func ReadUint64(data []byte) uint64 {
	return uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
		uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
}

func ReadInt64(data []byte) int64 {
	return int64(ReadUint64(data))
}
