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

package ubootenv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"os"
	"path/filepath"
)

// FIXME: add config option for that so that the user can select if
//        he/she wants env with or without flags
var headerSize = 5

// nativeEnv contains the data of the uboot environment stored in the native
// uboot format
type nativeEnv struct {
	fname string
	size  int
	data  map[string]string
}

// little endian helpers
func readUint32(data []byte) uint32 {
	var ret uint32
	buf := bytes.NewBuffer(data)
	binary.Read(buf, binary.LittleEndian, &ret)
	return ret
}

func writeUint32(u uint32) []byte {
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.LittleEndian, &u)
	return buf.Bytes()
}

// createNative creates a new empty uboot env file with the given size
func createNative(fname string, size int) (*nativeEnv, error) {
	f, err := os.Create(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := &nativeEnv{
		fname: fname,
		size:  size,
		data:  make(map[string]string),
	}

	return env, nil
}

func openNativeFormat(fname string, flags OpenFlags) (*nativeEnv, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	contentWithHeader, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	crc := readUint32(contentWithHeader)

	payload := contentWithHeader[headerSize:]
	actualCRC := crc32.ChecksumIEEE(payload)
	if crc != actualCRC {
		return nil, fmt.Errorf("cannot open %q: bad CRC %v != %v", fname, crc, actualCRC)
	}

	if eof := bytes.Index(payload, []byte{0, 0}); eof >= 0 {
		payload = payload[:eof]
	}

	data, err := parseData(payload, byte(0), flags)
	if err != nil {
		return nil, err
	}

	env := &nativeEnv{
		fname: fname,
		size:  len(contentWithHeader),
		data:  data,
	}

	return env, nil

}

// String returns a human-readable string representation. Notably, it delimits
// lines with '\n' instead of the normal native env delimiter '\0'
func (env *nativeEnv) String() string {
	buf := &bytes.Buffer{}
	writeData(buf, env.data, byte('\n'))
	return buf.String()
}

func (env *nativeEnv) Size() int {
	return env.size
}

// Get the value of the environment variable
func (env *nativeEnv) Get(name string) string {
	return env.data[name]
}

// Set an environment name to the given value, if the value is empty
// the variable will be removed from the environment
func (env *nativeEnv) Set(name, value string) {
	if name == "" {
		panic(fmt.Sprintf("Set() can not be called with empty key for value: %q", value))
	}
	if value == "" {
		delete(env.data, name)
		return
	}
	env.data[name] = value
}

// Save will write out the environment data
func (env *nativeEnv) Save() error {
	w := bytes.NewBuffer(nil)
	// will panic if the buffer can't grow, all writes to
	// the buffer will be ok because we sized it correctly
	w.Grow(env.size - headerSize)

	// write the payload
	writeData(w, env.data, byte(0))

	// write double \0 to mark the end of the env
	w.Write([]byte{0})

	// no keys, so no previous \0 was written so we write one here
	if len(env.data) == 0 {
		w.Write([]byte{0})
	}

	// write ff into the remaining parts
	writtenSoFar := w.Len()
	for i := 0; i < env.size-headerSize-writtenSoFar; i++ {
		w.Write([]byte{0xff})
	}

	// checksum
	crc := crc32.ChecksumIEEE(w.Bytes())

	// ensure dir sync
	dir, err := os.Open(filepath.Dir(env.fname))
	if err != nil {
		return err
	}
	defer dir.Close()

	// Note that we overwrite the existing file and do not do
	// the usual write-rename. The rationale is that we want to
	// minimize the amount of writes happening on a potential
	// FAT partition where the env is loaded from. The file will
	// always be of a fixed size so we know the writes will not
	// fail because of ENOSPC.
	//
	// The size of the env file never changes so we do not
	// truncate it.
	//
	// We also do not O_TRUNC to avoid reallocations on the FS
	// to minimize risk of fs corruption.
	f, err := os.OpenFile(env.fname, os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(writeUint32(crc)); err != nil {
		return err
	}
	// padding bytes (e.g. for redundant header)
	pad := make([]byte, headerSize-binary.Size(crc))
	if _, err := f.Write(pad); err != nil {
		return err
	}
	if _, err := f.Write(w.Bytes()); err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}

	return dir.Sync()
}
