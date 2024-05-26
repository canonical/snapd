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
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

// Env contains the data of the uboot environment
type Env struct {
	fname          string
	size           int
	headerFlagByte bool
	data           map[string]string
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

const sizeOfUint32 = 4

func calcHeaderSize(headerFlagByte bool) int {
	if headerFlagByte {
		// If uboot uses a header flag byte, header is 4 byte crc + flag byte
		return sizeOfUint32 + 1
	}
	// otherwise, just a 4 byte crc
	return sizeOfUint32
}

type CreateOptions struct {
	HeaderFlagByte bool
}

// Create a new empty uboot env file with the given size
func Create(fname string, size int, opts CreateOptions) (*Env, error) {
	f := mylog.Check2(os.Create(fname))

	defer f.Close()

	env := &Env{
		fname:          fname,
		size:           size,
		headerFlagByte: opts.HeaderFlagByte,
		data:           make(map[string]string),
	}

	return env, nil
}

// OpenFlags instructs open how to alter its behavior.
type OpenFlags int

const (
	// OpenBestEffort instructs OpenWithFlags to skip malformed data without returning an error.
	OpenBestEffort OpenFlags = 1 << iota
)

// Open opens a existing uboot env file
func Open(fname string) (*Env, error) {
	return OpenWithFlags(fname, OpenFlags(0))
}

// OpenWithFlags opens a existing uboot env file, passing additional flags.
func OpenWithFlags(fname string, flags OpenFlags) (*Env, error) {
	f := mylog.Check2(os.Open(fname))

	defer f.Close()

	contentWithHeader := mylog.Check2(io.ReadAll(f))

	// Most systems have SYS_REDUNDAND_ENVIRONMENT=y, so try that first
	tryHeaderFlagByte := true
	env := mylog.Check2(readEnv(contentWithHeader, flags, tryHeaderFlagByte))
	// if there is a bad CRC, maybe we just assumed the wrong header size
	if errors.Is(err, errBadCrc) {
		tryHeaderFlagByte := false
		env = mylog.Check2(readEnv(contentWithHeader, flags, tryHeaderFlagByte))
	}
	// if error was not one of the ones that might indicate we assumed the wrong
	// header size, or there is still an error after checking both header sizes
	// something is actually wrong

	env.fname = fname
	return env, nil
}

var errBadCrc = errors.New("bad CRC")

func readEnv(contentWithHeader []byte, flags OpenFlags, headerFlagByte bool) (*Env, error) {
	// The minimum valid env is 6 bytes (4 byte CRC + 2 null bytes for EOF)
	// The maximum header length is 5 bytes (4 byte CRC, + )
	// If we always make sure our env is 6 bytes long, we'll never run into
	// trouble doing some sort of OOB slicing below, but also we will
	// accept all legal envs
	if len(contentWithHeader) < 6 {
		return nil, errors.New("smaller than expected environment block")
	}

	headerSize := calcHeaderSize(headerFlagByte)

	crc := readUint32(contentWithHeader)

	payload := contentWithHeader[headerSize:]
	actualCRC := crc32.ChecksumIEEE(payload)
	if crc != actualCRC {
		return nil, fmt.Errorf("%w %v != %v", errBadCrc, crc, actualCRC)
	}

	if eof := bytes.Index(payload, []byte{0, 0}); eof >= 0 {
		payload = payload[:eof]
	}

	data := mylog.Check2(parseData(payload, flags))

	env := &Env{
		size:           len(contentWithHeader),
		headerFlagByte: headerFlagByte,
		data:           data,
	}

	return env, nil
}

func parseData(data []byte, flags OpenFlags) (map[string]string, error) {
	out := make(map[string]string)

	for _, envStr := range bytes.Split(data, []byte{0}) {
		if len(envStr) == 0 || envStr[0] == 0 || envStr[0] == 255 {
			continue
		}
		l := strings.SplitN(string(envStr), "=", 2)
		if len(l) != 2 || l[0] == "" {
			if flags&OpenBestEffort == OpenBestEffort {
				continue
			}
			return nil, fmt.Errorf("cannot parse line %q as key=value pair", envStr)
		}
		key := l[0]
		value := l[1]
		out[key] = value
	}

	return out, nil
}

func (env *Env) String() string {
	out := ""

	env.iterEnv(func(key, value string) {
		out += fmt.Sprintf("%s=%s\n", key, value)
	})

	return out
}

func (env *Env) Size() int {
	return env.size
}

func (env *Env) HeaderFlagByte() bool {
	return env.headerFlagByte
}

// Get the value of the environment variable
func (env *Env) Get(name string) string {
	return env.data[name]
}

// Set an environment name to the given value, if the value is empty
// the variable will be removed from the environment
func (env *Env) Set(name, value string) {
	if name == "" {
		panic(fmt.Sprintf("Set() can not be called with empty key for value: %q", value))
	}
	if value == "" {
		delete(env.data, name)
		return
	}
	env.data[name] = value
}

// iterEnv calls the passed function f with key, value for environment
// vars. The order is guaranteed (unlike just iterating over the map)
func (env *Env) iterEnv(f func(key, value string)) {
	keys := make([]string, 0, len(env.data))
	for k := range env.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if k == "" {
			panic("iterEnv iterating over a empty key")
		}

		f(k, env.data[k])
	}
}

// Save will write out the environment data
func (env *Env) Save() error {
	headerSize := calcHeaderSize(env.headerFlagByte)

	w := bytes.NewBuffer(nil)
	// will panic if the buffer can't grow, all writes to
	// the buffer will be ok because we sized it correctly
	w.Grow(env.size - headerSize)

	// write the payload
	env.iterEnv(func(key, value string) {
		w.Write([]byte(fmt.Sprintf("%s=%s", key, value)))
		w.Write([]byte{0})
	})

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
	dir := mylog.Check2(os.Open(filepath.Dir(env.fname)))

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
	f := mylog.Check2(os.OpenFile(env.fname, os.O_WRONLY, 0666))

	defer f.Close()
	mylog.Check2(f.Write(writeUint32(crc)))

	// padding bytes (e.g. for redundant header)
	pad := make([]byte, headerSize-binary.Size(crc))
	mylog.Check2(f.Write(pad))
	mylog.Check2(f.Write(w.Bytes()))
	mylog.Check(f.Sync())

	return dir.Sync()
}

// Import is a helper that imports a given text file that contains
// "key=value" paris into the uboot env. Lines starting with ^# are
// ignored (like the input file on mkenvimage)
func (env *Env) Import(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || len(line) == 0 {
			continue
		}
		l := strings.SplitN(line, "=", 2)
		if len(l) == 1 {
			return fmt.Errorf("Invalid line: %q", line)
		}
		env.data[l[0]] = l[1]

	}

	return scanner.Err()
}
