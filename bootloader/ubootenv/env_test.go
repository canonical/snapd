// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package ubootenv_test

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/ubootenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type uenvTestSuite struct {
	envFile string
}

var _ = Suite(&uenvTestSuite{})

func (u *uenvTestSuite) SetUpTest(c *C) {
	u.envFile = filepath.Join(c.MkDir(), "uboot.env")
}

func (u *uenvTestSuite) TestSetNoDuplicate(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	env.Set("foo", "bar")
	env.Set("foo", "bar")
	c.Assert(env.String(), Equals, "foo=bar\n")
}

func (u *uenvTestSuite) TestOpenEnv(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	env.Set("foo", "bar")
	c.Assert(env.String(), Equals, "foo=bar\n")
	err = env.Save()
	c.Assert(err, IsNil)

	env2, err := ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env2.String(), Equals, "foo=bar\n")
}

func (u *uenvTestSuite) TestOpenEnvNoHeaderFlagByte(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: false})
	c.Assert(err, IsNil)
	env.Set("foo", "bar")
	c.Assert(env.String(), Equals, "foo=bar\n")
	err = env.Save()
	c.Assert(err, IsNil)

	env2, err := ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env2.String(), Equals, "foo=bar\n")
}

func (u *uenvTestSuite) TestOpenEnvBadEmpty(c *C) {
	empty := filepath.Join(c.MkDir(), "empty.env")

	err := os.WriteFile(empty, nil, 0644)
	c.Assert(err, IsNil)

	_, err = ubootenv.Open(empty)
	c.Assert(err, ErrorMatches, `cannot open ".*": smaller than expected environment block`)
}

func (u *uenvTestSuite) TestOpenEnvBadCRC(c *C) {
	corrupted := filepath.Join(c.MkDir(), "corrupted.env")

	buf := make([]byte, 4096)
	err := os.WriteFile(corrupted, buf, 0644)
	c.Assert(err, IsNil)

	_, err = ubootenv.Open(corrupted)
	c.Assert(err, ErrorMatches, `cannot open ".*": bad CRC 0 != .*`)
}

func (u *uenvTestSuite) TestGetSimple(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	env.Set("foo", "bar")
	c.Assert(env.Get("foo"), Equals, "bar")
}

func (u *uenvTestSuite) TestGetNoSuchEntry(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	c.Assert(env.Get("no-such-entry"), Equals, "")
}

func (u *uenvTestSuite) TestImport(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)

	r := strings.NewReader("foo=bar\n#comment\n\nbaz=baz")
	err = env.Import(r)
	c.Assert(err, IsNil)
	// order is alphabetic
	c.Assert(env.String(), Equals, "baz=baz\nfoo=bar\n")
}

func (u *uenvTestSuite) TestImportHasError(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)

	r := strings.NewReader("foxy")
	err = env.Import(r)
	c.Assert(err, ErrorMatches, "Invalid line: \"foxy\"")
}

func (u *uenvTestSuite) TestSetEmptyUnsets(c *C) {
	env, err := ubootenv.Create(u.envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)

	env.Set("foo", "bar")
	c.Assert(env.String(), Equals, "foo=bar\n")
	env.Set("foo", "")
	c.Assert(env.String(), Equals, "")
}

func (u *uenvTestSuite) makeUbootEnvFromData(c *C, mockData []byte, useHeaderFlagByte bool) {
	w := bytes.NewBuffer(nil)
	crc := crc32.ChecksumIEEE(mockData)
	w.Write(ubootenv.WriteUint32(crc))
	if useHeaderFlagByte {
		w.Write([]byte{0})
	}
	w.Write(mockData)

	f, err := os.Create(u.envFile)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Write(w.Bytes())
	c.Assert(err, IsNil)
}

// ensure that the data after \0\0 is discarded (except for crc)
func (u *uenvTestSuite) TestReadStopsAfterDoubleNull(c *C) {
	mockData := []byte{
		// foo=bar
		0x66, 0x6f, 0x6f, 0x3d, 0x62, 0x61, 0x72,
		// eof
		0x00, 0x00,
		// junk after eof as written by fw_setenv sometimes
		// =b
		0x3d, 62,
		// empty
		0xff, 0xff,
	}
	u.makeUbootEnvFromData(c, mockData, true)

	env, err := ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "foo=bar\n")
	c.Assert(env.HeaderFlagByte(), Equals, true)

	u.makeUbootEnvFromData(c, mockData, false)

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "foo=bar\n")
	c.Assert(env.HeaderFlagByte(), Equals, false)
}

// ensure that the malformed data is not causing us to panic.
func (u *uenvTestSuite) TestErrorOnMalformedData(c *C) {
	mockData := []byte{
		// foo
		0x66, 0x6f, 0x6f,
		// eof
		0x00, 0x00,
	}
	u.makeUbootEnvFromData(c, mockData, true)

	env, err := ubootenv.Open(u.envFile)
	c.Assert(err, ErrorMatches, `cannot open ".*": cannot parse line "foo" as key=value pair`)
	c.Assert(env, IsNil)

	u.makeUbootEnvFromData(c, mockData, false)

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, ErrorMatches, `cannot open ".*": cannot parse line "foo" as key=value pair`)
	c.Assert(env, IsNil)
}

// ensure that the malformed data is not causing us to panic.
func (u *uenvTestSuite) TestOpenBestEffort(c *C) {
	testCases := map[string][]byte{"noise": {
		// key1=value1
		0x6b, 0x65, 0x79, 0x31, 0x3d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x31, 0x00,
		// foo
		0x66, 0x6f, 0x6f, 0x00,
		// key2=value2
		0x6b, 0x65, 0x79, 0x32, 0x3d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x32, 0x00,
		// eof
		0x00, 0x00,
	}, "no-eof": {
		// key1=value1
		0x6b, 0x65, 0x79, 0x31, 0x3d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x31, 0x00,
		// key2=value2
		0x6b, 0x65, 0x79, 0x32, 0x3d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x32, 0x00,
		// NO EOF!
	}, "noise-eof": {
		// key1=value1
		0x6b, 0x65, 0x79, 0x31, 0x3d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x31, 0x00,
		// key2=value2
		0x6b, 0x65, 0x79, 0x32, 0x3d, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x32, 0x00,
		// foo
		0x66, 0x6f, 0x6f, 0x00,
	}}
	for testName, mockData := range testCases {
		u.makeUbootEnvFromData(c, mockData, true)

		env, err := ubootenv.OpenWithFlags(u.envFile, ubootenv.OpenBestEffort)
		c.Assert(err, IsNil, Commentf(testName))
		c.Check(env.String(), Equals, "key1=value1\nkey2=value2\n", Commentf(testName))
		c.Assert(env.HeaderFlagByte(), Equals, true)

		u.makeUbootEnvFromData(c, mockData, false)

		env, err = ubootenv.OpenWithFlags(u.envFile, ubootenv.OpenBestEffort)
		c.Assert(err, IsNil, Commentf(testName))
		c.Check(env.String(), Equals, "key1=value1\nkey2=value2\n", Commentf(testName))
		c.Assert(env.HeaderFlagByte(), Equals, false)
	}
}

func (u *uenvTestSuite) TestErrorOnMissingKeyInKeyValuePair(c *C) {
	mockData := []byte{
		// =foo
		0x3d, 0x66, 0x6f, 0x6f,
		// eof
		0x00, 0x00,
	}
	u.makeUbootEnvFromData(c, mockData, true)

	env, err := ubootenv.Open(u.envFile)
	c.Assert(err, ErrorMatches, `cannot open ".*": cannot parse line "=foo" as key=value pair`)
	c.Assert(env, IsNil)

	u.makeUbootEnvFromData(c, mockData, false)

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, ErrorMatches, `cannot open ".*": cannot parse line "=foo" as key=value pair`)
	c.Assert(env, IsNil)
}

func (u *uenvTestSuite) TestReadEmptyFile(c *C) {
	mockData := []byte{
		// eof
		0x00, 0x00,
		// empty
		0xff, 0xff,
	}
	u.makeUbootEnvFromData(c, mockData, true)

	env, err := ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "")
	c.Assert(env.HeaderFlagByte(), Equals, true)

	u.makeUbootEnvFromData(c, mockData, false)

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "")
	c.Assert(env.HeaderFlagByte(), Equals, false)
}

func (u *uenvTestSuite) TestWritesEmptyFileWithDoubleNewline(c *C) {
	env, err := ubootenv.Create(u.envFile, 12, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)

	r, err := os.Open(u.envFile)
	c.Assert(err, IsNil)
	defer r.Close()
	content, err := io.ReadAll(r)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte{
		// crc
		0x11, 0x38, 0xb3, 0x89,
		// redundant
		0x0,
		// eof
		0x0, 0x0,
		// footer
		0xff, 0xff, 0xff, 0xff, 0xff,
	})

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "")
	c.Assert(env.HeaderFlagByte(), Equals, true)
}

func (u *uenvTestSuite) TestWritesEmptyFileWithDoubleNewlineNoHeaderFlagByte(c *C) {
	env, err := ubootenv.Create(u.envFile, 11, ubootenv.CreateOptions{HeaderFlagByte: false})
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)

	r, err := os.Open(u.envFile)
	c.Assert(err, IsNil)
	defer r.Close()
	content, err := io.ReadAll(r)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte{
		// crc
		0x11, 0x38, 0xb3, 0x89,
		// eof
		0x0, 0x0,
		// footer
		0xff, 0xff, 0xff, 0xff, 0xff,
	})

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "")
	c.Assert(env.HeaderFlagByte(), Equals, false)
}

func (u *uenvTestSuite) TestWritesContentCorrectly(c *C) {
	totalSize := 16

	env, err := ubootenv.Create(u.envFile, totalSize, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	env.Set("a", "b")
	env.Set("c", "d")
	err = env.Save()
	c.Assert(err, IsNil)

	r, err := os.Open(u.envFile)
	c.Assert(err, IsNil)
	defer r.Close()
	content, err := io.ReadAll(r)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte{
		// crc
		0xc7, 0xd9, 0x6b, 0xc5,
		// redundant
		0x0,
		// a=b
		0x61, 0x3d, 0x62,
		// eol
		0x0,
		// c=d
		0x63, 0x3d, 0x64,
		// eof
		0x0, 0x0,
		// footer
		0xff, 0xff,
	})

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "a=b\nc=d\n")
	c.Assert(env.Size(), Equals, totalSize)
	c.Assert(env.HeaderFlagByte(), Equals, true)
}

func (u *uenvTestSuite) TestWritesContentCorrectlyNoHeaderFlagByte(c *C) {
	totalSize := 15

	env, err := ubootenv.Create(u.envFile, totalSize, ubootenv.CreateOptions{HeaderFlagByte: false})
	c.Assert(err, IsNil)
	env.Set("a", "b")
	env.Set("c", "d")
	err = env.Save()
	c.Assert(err, IsNil)

	r, err := os.Open(u.envFile)
	c.Assert(err, IsNil)
	defer r.Close()
	content, err := io.ReadAll(r)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte{
		// crc
		0xc7, 0xd9, 0x6b, 0xc5,
		// a=b
		0x61, 0x3d, 0x62,
		// eol
		0x0,
		// c=d
		0x63, 0x3d, 0x64,
		// eof
		0x0, 0x0,
		// footer
		0xff, 0xff,
	})

	env, err = ubootenv.Open(u.envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "a=b\nc=d\n")
	c.Assert(env.Size(), Equals, totalSize)
	c.Assert(env.HeaderFlagByte(), Equals, false)
}

func (u *uenvTestSuite) TestRedundantFlagByteWraparound(c *C) {
	// Test that flag byte comparison handles wraparound correctly
	// (e.g., flag=1 should be considered newer than flag=255 after wrap)
	env, err := ubootenv.CreateRedundant(u.envFile, ubootenv.DefaultRedundantEnvSize)
	c.Assert(err, IsNil)

	// Save many times to get close to wraparound
	// Start at flag=1, need to get to flag=255, then wrap to 0
	for i := 0; i < 256; i++ {
		env.Set("counter", fmt.Sprintf("%d", i))
		err = env.Save()
		c.Assert(err, IsNil)
	}

	// After 256 saves starting from flag=1, we should have wrapped
	env2, err := ubootenv.OpenRedundant(u.envFile, ubootenv.DefaultRedundantEnvSize)
	c.Assert(err, IsNil)
	c.Assert(env2.Get("counter"), Equals, "255")
}

func (u *uenvTestSuite) TestRedundantOffsets(c *C) {
	copy1, copy2 := ubootenv.RedundantOffsets(ubootenv.DefaultRedundantEnvSize)
	c.Assert(copy1, Equals, int64(0))
	c.Assert(copy2, Equals, int64(ubootenv.DefaultRedundantEnvSize))
}

func (u *uenvTestSuite) TestRedundantAlternatesCopies(c *C) {
	// Test that saves alternate between copy1 and copy2
	size := ubootenv.DefaultRedundantEnvSize
	env, err := ubootenv.CreateRedundant(u.envFile, size)
	c.Assert(err, IsNil)

	// Helper to read flag bytes from both copies
	readFlags := func() (byte, byte) {
		data, err := os.ReadFile(u.envFile)
		c.Assert(err, IsNil)
		// Flag byte is at offset 4 (after CRC) in each copy
		return data[4], data[size+4]
	}

	// After CreateRedundant + initial Save(), copy2 should have flag 1
	flag1, flag2 := readFlags()
	c.Assert(flag1, Equals, byte(0), Commentf("copy1 flag after create"))
	c.Assert(flag2, Equals, byte(1), Commentf("copy2 flag after create"))

	// Second save should write to copy1 with flag 2
	env.Set("key", "value1")
	err = env.Save()
	c.Assert(err, IsNil)
	flag1, flag2 = readFlags()
	c.Assert(flag1, Equals, byte(2), Commentf("copy1 flag after 2nd save"))
	c.Assert(flag2, Equals, byte(1), Commentf("copy2 flag after 2nd save"))

	// Third save should write to copy2 with flag 3
	env.Set("key", "value2")
	err = env.Save()
	c.Assert(err, IsNil)
	flag1, flag2 = readFlags()
	c.Assert(flag1, Equals, byte(2), Commentf("copy1 flag after 3rd save"))
	c.Assert(flag2, Equals, byte(3), Commentf("copy2 flag after 3rd save"))

	// Fourth save should write to copy1 with flag 4
	env.Set("key", "value3")
	err = env.Save()
	c.Assert(err, IsNil)
	flag1, flag2 = readFlags()
	c.Assert(flag1, Equals, byte(4), Commentf("copy1 flag after 4th save"))
	c.Assert(flag2, Equals, byte(3), Commentf("copy2 flag after 4th save"))
}

// makeRedundantEnvWithFlags creates a redundant environment file where
// copy1 has flag1 and copy2 has flag2. Both copies contain the same data.
func (u *uenvTestSuite) makeRedundantEnvWithFlags(c *C, size int, flag1, flag2 byte, data []byte) {
	// Build a single copy: CRC32 (4 bytes) + flag (1 byte) + payload
	buildCopy := func(flag byte) []byte {
		buf := make([]byte, size)
		// Fill with 0xff, then overlay the header and data
		for i := range buf {
			buf[i] = 0xff
		}
		copy(buf[5:], data)

		// CRC is computed over payload only (after header)
		payload := buf[5:]
		crc := crc32.ChecksumIEEE(payload)
		copy(buf[0:4], ubootenv.WriteUint32(crc))
		buf[4] = flag
		return buf
	}

	copy1Data := buildCopy(flag1)
	copy2Data := buildCopy(flag2)

	f, err := os.Create(u.envFile)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Write(copy1Data)
	c.Assert(err, IsNil)
	_, err = f.Write(copy2Data)
	c.Assert(err, IsNil)
}

func (u *uenvTestSuite) TestRedundantSaveInvertedFlags(c *C) {
	// This test verifies that Save() writes to the inactive copy even when
	// the flag pattern is inverted from what the code might assume.
	//
	// Normal pattern: copy1 has even flags, copy2 has odd flags
	// Inverted pattern: copy1 has odd flags (5), copy2 has even flags (4)
	//
	// With flag1=5 and flag2=4, copy1 is active (higher flag).
	// Save() should write to copy2 (the inactive copy).
	// Bug: if Save() uses odd/even to determine which copy to write,
	// it will incorrectly write to copy1 (overwriting the active copy).

	size := ubootenv.DefaultRedundantEnvSize

	// Create sample env data
	envData := []byte("key=original\x00\x00")

	// Create a redundant env with an inverted flag pattern:
	// copy1 = flag 5 (odd, active), copy2 = flag 4 (even, inactive)
	u.makeRedundantEnvWithFlags(c, size, 5, 4, envData)

	// Open the redundant environment
	env, err := ubootenv.OpenRedundant(u.envFile, size)
	c.Assert(err, IsNil)

	// Verify we read the correct data
	c.Assert(env.Get("key"), Equals, "original")

	// Modify and save
	env.Set("key", "modified")
	err = env.Save()
	c.Assert(err, IsNil)

	// Read the raw file to check which copy was written
	data, err := os.ReadFile(u.envFile)
	c.Assert(err, IsNil)

	// Flag bytes are at offset 4 in each copy
	flag1 := data[4]
	flag2 := data[size+4]

	// After save, the inactive copy (copy2) should have the new flag (6)
	// and copy1 should be unchanged (5)
	c.Assert(flag1, Equals, byte(5), Commentf("copy1 flag should remain 5 (was active, not written)"))
	c.Assert(flag2, Equals, byte(6), Commentf("copy2 flag should be 6 (was inactive, now updated)"))
}
