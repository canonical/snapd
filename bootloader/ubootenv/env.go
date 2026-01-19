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

// Package ubootenv implements reading and writing of U-Boot environment files.
//
// # Single Environment Format
//
// The basic U-Boot environment format consists of:
//   - 4-byte CRC32 (IEEE polynomial, little-endian)
//   - Optional 1-byte flag (present when SYS_REDUNDAND_ENVIRONMENT is enabled)
//   - Data section containing null-terminated "key=value" strings
//   - Double null byte (0x00 0x00) marking end of data
//   - Remaining space filled with 0xff bytes
//
// # Redundant Environment Format
//
// When U-Boot's CONFIG_SYS_REDUNDAND_ENVIRONMENT is enabled, the environment consists of
// two identical copies stored consecutively, i.e. CONFIG_ENV_OFFSET_REDUND is set so that
// the second copy follows the first.
//
// Each copy has the format above with the 1-byte flag present. The flag byte determines
// which copy is active:
//   - The copy with the higher flag value is considered active (but 0 is higher than 255)
//   - When saving, the inactive copy is written first with flag = active_flag + 1
//   - This provides atomic updates: if power is lost during write, the old
//     copy remains valid
//   - If one copy has a bad CRC, the other copy is used (failover)
//
// For more details, see the U-Boot documentation:
// https://docs.u-boot.org/en/latest/usage/environment.html
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

	"github.com/snapcore/snapd/logger"
)

// Env contains the data of the uboot environment
type Env struct {
	fname          string
	size           int
	headerFlagByte bool
	data           map[string]string

	// redundant mode fields
	redundant  bool // true if using redundant mode with two copies
	activeFlag byte // flag value of the active copy (initially FlagActive or FlagObsolete)
	activeCopy int  // which copy is active (Copy1 or Copy2)
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

// Constants for redundant environment support
const (
	// DefaultRedundantEnvSize is the default size for each copy in a redundant environment (8KiB)
	DefaultRedundantEnvSize = 8192
	// FlagActive marks an environment copy as the current active one
	FlagActive = 0x01
	// FlagObsolete marks an environment copy as obsolete/backup
	FlagObsolete = 0x00
	// Copy1 identifies the first environment copy
	Copy1 = 1
	// Copy2 identifies the second environment copy
	Copy2 = 2
)

// redundantOffsets returns the byte offsets for the two environment copies.
func redundantOffsets(size int) (copy1Offset, copy2Offset int64) {
	return 0, int64(size)
}

// readEnvCopy reads a single environment copy from the device.
// Returns nil if the read fails.
func readEnvCopy(f *os.File, offset int64, size int) []byte {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(f, buf)
	if err != nil {
		return nil
	}
	if n < size {
		return nil
	}
	return buf
}

// isNewerFlag returns true if flag1 is newer than flag2 (handles wraparound).
func isNewerFlag(flag1, flag2 byte) bool {
	return int8(flag1-flag2) >= 0
}

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
	f, err := os.Create(fname)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()

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
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	contentWithHeader, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	// Most systems have SYS_REDUNDAND_ENVIRONMENT=y, so try that first
	tryHeaderFlagByte := true
	env, err := readEnv(contentWithHeader, flags, tryHeaderFlagByte)
	// if there is a bad CRC, maybe we just assumed the wrong header size
	if errors.Is(err, errBadCrc) {
		tryHeaderFlagByte := false
		env, err = readEnv(contentWithHeader, flags, tryHeaderFlagByte)
	}
	// if error was not one of the ones that might indicate we assumed the wrong
	// header size, or there is still an error after checking both header sizes
	// something is actually wrong
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", fname, err)
	}

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

	data, err := parseData(payload, flags)
	if err != nil {
		return nil, err
	}

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

// buildPayload builds the environment payload and returns it along with its CRC.
func (env *Env) buildPayload() ([]byte, uint32) {
	headerSize := calcHeaderSize(env.headerFlagByte)

	w := bytes.NewBuffer(nil)
	w.Grow(env.size - headerSize)

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

	// write 0xff into the remaining parts
	writtenSoFar := w.Len()
	for i := 0; i < env.size-headerSize-writtenSoFar; i++ {
		w.Write([]byte{0xff})
	}

	payload := w.Bytes()
	return payload, crc32.ChecksumIEEE(payload)
}

// writeToDevice opens a device, optionally checks the size, writes the buffer
// at the given offset, and syncs.
func writeToDevice(fname string, buf []byte, offset int64, minimumSize int64) error {
	f, err := os.OpenFile(fname, os.O_RDWR, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < minimumSize {
		return fmt.Errorf("device too small: got %d bytes, need %d", fi.Size(), minimumSize)
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.Write(buf); err != nil {
		return err
	}
	return f.Sync()
}

// buildImage builds a complete environment image with header (CRC + flag) and payload.
func (env *Env) buildImage(flag byte) []byte {
	payload, crc := env.buildPayload()

	buf := make([]byte, env.size)
	copy(buf[0:4], writeUint32(crc))
	if env.headerFlagByte {
		buf[4] = flag
		copy(buf[5:], payload)
	} else {
		copy(buf[4:], payload)
	}
	return buf
}

// Save will write out the environment data
func (env *Env) Save() error {
	if !env.redundant {
		return env.saveLegacy()
	}
	return env.saveRedundant()
}

// saveLegacy writes the environment to a single-copy file.
func (env *Env) saveLegacy() error {
	buf := env.buildImage(0)

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
	if err := writeToDevice(env.fname, buf, 0, int64(env.size)); err != nil {
		return err
	}

	// Sync the directory as well for FAT filesystems
	dir, err := os.Open(filepath.Dir(env.fname))
	if err != nil {
		return err
	}
	defer dir.Close()

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

// CreateRedundant creates a new redundant U-Boot environment image with two copies.
// This is used at prepare-image time to create an image that will be written to
// a raw partition. The image will be 2*size bytes total, with two environment copies.
// Both copies are initialized as empty with the first copy marked active.
func CreateRedundant(fname string, size int) (*Env, error) {
	f, err := os.Create(fname)
	if err != nil {
		return nil, err
	}
	// Pre-allocate file to full size (2 copies)
	if err := f.Truncate(int64(size * 2)); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()

	env := &Env{
		fname:          fname,
		size:           size,
		headerFlagByte: true, // redundant environments always have the flag byte
		data:           make(map[string]string),
		redundant:      true,
		activeFlag:     0, // first Save() will write flag 1
		activeCopy:     Copy1, // first Save() will write to Copy2
	}

	// Write initial empty environment with two copies
	if err := env.Save(); err != nil {
		return nil, err
	}

	return env, nil
}

// OpenRedundant opens an existing redundant U-Boot environment from a raw device.
// It reads both copies and selects the active one based on CRC validity and flag bytes.
func OpenRedundant(devname string, size int) (*Env, error) {
	return OpenRedundantWithFlags(devname, size, OpenFlags(0))
}

// OpenRedundantWithFlags opens a redundant environment from a raw device with additional flags.
func OpenRedundantWithFlags(devname string, size int, flags OpenFlags) (*Env, error) {
	f, err := os.Open(devname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	copy1Offset, copy2Offset := redundantOffsets(size)

	// Read each copy separately so that a read error on one copy
	// doesn't prevent us from trying the other
	copy1 := readEnvCopy(f, copy1Offset, size)
	copy2 := readEnvCopy(f, copy2Offset, size)

	// If both reads failed completely, return error
	if copy1 == nil && copy2 == nil {
		return nil, fmt.Errorf("redundant environment device too small or unreadable")
	}

	// Try to parse both copies
	var env1, env2 *Env
	var err1, err2 error

	if copy1 != nil {
		env1, err1 = readEnv(copy1, flags, true)
	} else {
		err1 = fmt.Errorf("copy1 unreadable")
	}

	if copy2 != nil {
		env2, err2 = readEnv(copy2, flags, true)
	} else {
		err2 = fmt.Errorf("copy2 unreadable")
	}

	// Select the active copy based on validity and flag byte
	var env *Env
	var activeFlag byte
	var activeCopy int

	switch {
	case err1 == nil && err2 == nil:
		// Both valid - choose based on flag byte
		// The flag byte is at offset 4 (after CRC)
		flag1 := copy1[sizeOfUint32]
		flag2 := copy2[sizeOfUint32]
		if isNewerFlag(flag1, flag2) {
			env = env1
			activeFlag = flag1
			activeCopy = Copy1
		} else {
			env = env2
			activeFlag = flag2
			activeCopy = Copy2
		}
	case err1 == nil:
		// Only copy1 valid
		logger.Noticef("redundant environment copy2 is invalid: %v", err2)
		env = env1
		activeFlag = copy1[sizeOfUint32]
		activeCopy = Copy1
	case err2 == nil:
		// Only copy2 valid
		logger.Noticef("redundant environment copy1 is invalid: %v", err1)
		env = env2
		activeFlag = copy2[sizeOfUint32]
		activeCopy = Copy2
	default:
		// Both invalid
		return nil, fmt.Errorf("cannot open redundant environment %q: both copies invalid: copy1: %v, copy2: %v", devname, err1, err2)
	}

	env.fname = devname
	env.size = size
	env.redundant = true
	env.activeFlag = activeFlag
	env.activeCopy = activeCopy

	return env, nil
}

// Redundant returns true if this environment uses redundant mode.
func (env *Env) Redundant() bool {
	return env.redundant
}

// saveRedundant writes the environment to the inactive copy first,
// then updates the flag byte to make it active.
func (env *Env) saveRedundant() error {
	copy1Offset, copy2Offset := redundantOffsets(env.size)

	// Determine which copy to write to (the inactive one)
	// and what the new flag value should be
	var writeOffset int64
	var newActiveCopy int

	// Increment the flag value for the new active copy (wraps from 255 to 0)
	newFlag := env.activeFlag + 1

	// Write to the inactive copy based on which copy is currently active
	if env.activeCopy == Copy1 {
		writeOffset = copy2Offset
		newActiveCopy = Copy2
	} else {
		writeOffset = copy1Offset
		newActiveCopy = Copy1
	}

	buf := env.buildImage(newFlag)
	expectedSize := int64(env.size * 2)

	if err := writeToDevice(env.fname, buf, writeOffset, expectedSize); err != nil {
		return err
	}

	// Update our tracking of the active copy and flag
	env.activeFlag = newFlag
	env.activeCopy = newActiveCopy

	return nil
}
