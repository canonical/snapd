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
	"io/ioutil"
	"os"
)

type xauth struct {
	Family  uint16
	Address []byte
	Number  []byte
	Name    []byte
	Data    []byte
}

func readChunk(f *os.File) ([]byte, error) {
	b := make([]byte, 2)
	n, err := f.Read(b)
	if err != nil {
		return nil, err
	} else if n != 2 {
		return nil, fmt.Errorf("Could not read enough bytes")
	}

	size := int(binary.BigEndian.Uint16(b))
	chunk := make([]byte, size)
	n, err = f.Read(chunk)
	if err != nil {
		return nil, err
	} else if n != size {
		return nil, fmt.Errorf("Could not read enough bytes")
	}

	return chunk, nil
}

func (xa *xauth) ReadFromFile(f *os.File) error {
	b := make([]byte, 2)
	_, err := f.Read(b)
	if err != nil {
		return err
	}
	xa.Family = binary.BigEndian.Uint16(b)

	xa.Address, err = readChunk(f)
	if err != nil {
		return err
	}

	xa.Number, err = readChunk(f)
	if err != nil {
		return err
	}

	xa.Name, err = readChunk(f)
	if err != nil {
		return err
	}

	xa.Data, err = readChunk(f)
	if err != nil {
		return err
	}

	return nil
}

func ValidateXauthority(path string) (int, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0600)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	cookies := 0
	for {
		xa := &xauth{}
		err = xa.ReadFromFile(f)
		if err == io.EOF {
			break
		} else if err != nil {
			return 0, err
		}
		// FIXME we can do further validation of the cookies like
		// checking for valid families etc.
		cookies += 1
	}

	return cookies, nil
}

func MockXauthority(cookies int) string {
	f, _ := ioutil.TempFile("", "xauth")
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
		f.Write(data)
	}
	f.Close()
	return f.Name()
}
