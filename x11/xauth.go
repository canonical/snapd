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
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return nil, err
	}

	size := int(binary.BigEndian.Uint16(b[:]))
	chunk := make([]byte, size)
	if _, err := io.ReadFull(r, chunk); err != nil {
		return nil, err
	}

	return chunk, nil
}

func (xa *xauth) readFromFile(r io.Reader) error {
	b := [2]byte{}
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return err
	}
	// The family field consists of two bytes
	xa.Family = binary.BigEndian.Uint16(b[:])

	var err error

	if xa.Address, err = readChunk(r); err != nil {
		return err
	}

	if xa.Number, err = readChunk(r); err != nil {
		return err
	}

	if xa.Name, err = readChunk(r); err != nil {
		return err
	}

	if xa.Data, err = readChunk(r); err != nil {
		return err
	}

	return nil
}

// ValidateXauthority validates a given Xauthority file. The file is valid
// if it can be parsed and contains at least one cookie.
func ValidateXauthorityFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
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
		} else if err != nil {
			return err
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
	f, err := os.CreateTemp("", "xauth")
	if err != nil {
		return "", err
	}
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
		m, err := f.Write(data)
		if err != nil {
			return "", err
		} else if m != len(data) {
			return "", fmt.Errorf("Could write cookie")
		}
	}
	return f.Name(), nil
}
