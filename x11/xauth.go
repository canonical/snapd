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

package x11

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/ddkwork/golibrary/mylog"
)

// See https://cgit.freedesktop.org/xorg/lib/libXau/tree/AuRead.c and
// https://cgit.freedesktop.org/xorg/lib/libXau/tree/include/X11/Xauth.h
// for details about the actual file format.
type xauth struct {
	Family  uint16
	Address []byte
	Number  []byte
	Name    []byte
	Data    []byte
}

func readChunk(r io.Reader) ([]byte, error) {
	// A chunk consists of a length encoded by two bytes (so max 64K)
	// and additional data which is the real value of the item we're
	// reading here from the file.

	b := [2]byte{}
	mylog.Check2(io.ReadFull(r, b[:]))

	size := int(binary.BigEndian.Uint16(b[:]))
	chunk := make([]byte, size)
	mylog.Check2(io.ReadFull(r, chunk))

	return chunk, nil
}

func (xa *xauth) readFromFile(r io.Reader) error {
	b := [2]byte{}
	mylog.Check2(io.ReadFull(r, b[:]))

	// The family field consists of two bytes
	xa.Family = binary.BigEndian.Uint16(b[:])
	xa.Address = mylog.Check2(readChunk(r))
	xa.Number = mylog.Check2(readChunk(r))
	xa.Name = mylog.Check2(readChunk(r))
	xa.Data = mylog.Check2(readChunk(r))

	return nil
}

// ValidateXauthority validates a given Xauthority file. The file is valid
// if it can be parsed and contains at least one cookie.
func ValidateXauthorityFile(path string) error {
	f := mylog.Check2(os.Open(path))

	defer f.Close()
	return ValidateXauthority(f)
}

// ValidateXauthority validates a given Xauthority file. The file is valid
// if it can be parsed and contains at least one cookie.
func ValidateXauthority(r io.Reader) error {
	cookies := 0
	for {
		xa := &xauth{}
		err := xa.readFromFile(r)
		if err == io.EOF {
			break
		}

		cookies++
	}

	if cookies <= 0 {
		return fmt.Errorf("Xauthority file is invalid")
	}

	return nil
}

// MockXauthority will create a fake xauthority file and place it
// on a temporary path which is returned as result.
func MockXauthority(cookies int) (string, error) {
	f := mylog.Check2(os.CreateTemp("", "xauth"))

	defer f.Close()
	for n := 0; n < cookies; n++ {
		data := []byte{
			// Family
			0x01, 0x00,
			// Address
			0x00, 0x04, 0x73, 0x6e, 0x61, 0x70,
			// Number
			0x00, 0x01, 0xff,
			// Name
			0x00, 0x05, 0x73, 0x6e, 0x61, 0x70, 0x64,
			// Data
			0x00, 0x01, 0xff,
		}
		mylog.Check2(f.Write(data))

	}
	return f.Name(), nil
}
