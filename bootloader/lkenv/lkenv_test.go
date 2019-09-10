// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package lkenv_test

import (
	"bytes"
	"compress/gzip"
	. "gopkg.in/check.v1"
	"io"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/bootloader/lkenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type lkenvTestSuite struct {
	envPath    string
	envPathbak string
}

var _ = Suite(&lkenvTestSuite{})

func (l *lkenvTestSuite) SetUpTest(c *C) {
	l.envPath = filepath.Join(c.MkDir(), "snapbootsel.bin")
	l.envPathbak = l.envPath + "bak"
}

// unpack test data packed with gzip
func unpackTestData(data []byte) (resData []byte, err error) {
	b := bytes.NewBuffer(data)
	var r io.Reader
	r, err = gzip.NewReader(b)
	if err != nil {
		return
	}
	var env bytes.Buffer
	_, err = env.ReadFrom(r)
	if err != nil {
		return
	}
	return env.Bytes(), nil
}

func (l *lkenvTestSuite) TestSet(c *C) {
	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	env.Set("snap_mode", "try")
	c.Check(env.Get("snap_mode"), Equals, "try")
}

func (l *lkenvTestSuite) TestSave(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPathbak, buf, 0644)
	c.Assert(err, IsNil)
	l.TestSaveNoBak(c)
}

func (l *lkenvTestSuite) TestCtoGoString(c *C) {
	for _, t := range []struct {
		input    []byte
		expected string
	}{
		{[]byte{0, 0, 0, 0, 0}, ""},
		{[]byte{'a', 0, 0, 0, 0}, "a"},
		{[]byte{'a', 'b', 0, 0, 0}, "ab"},
		{[]byte{'a', 'b', 'c', 0, 0}, "abc"},
		{[]byte{'a', 'b', 'c', 'd', 0}, "abcd"},
		// no trailing \0 - assume corrupted "" ?
		{[]byte{'a', 'b', 'c', 'd', 'e'}, ""},
		// first \0 is the cutof
		{[]byte{'a', 'b', 0, 'z', 0}, "ab"},
	} {
		c.Check(lkenv.CToGoString(t.input), Equals, t.expected)
	}

}

func (l *lkenvTestSuite) TestCopyStringHappy(c *C) {
	for _, t := range []struct {
		input    string
		expected []byte
	}{
		// input up to the size of the buffer works
		{"", []byte{0, 0, 0, 0, 0}},
		{"a", []byte{'a', 0, 0, 0, 0}},
		{"ab", []byte{'a', 'b', 0, 0, 0}},
		{"abc", []byte{'a', 'b', 'c', 0, 0}},
		{"abcd", []byte{'a', 'b', 'c', 'd', 0}},
		// only what fit is copied
		{"abcde", []byte{'a', 'b', 'c', 'd', 0}},
		{"abcdef", []byte{'a', 'b', 'c', 'd', 0}},
		// strange embedded stuff works
		{"ab\000z", []byte{'a', 'b', 0, 'z', 0}},
	} {
		b := make([]byte, 5)
		lkenv.CopyString(b, t.input)
		c.Check(b, DeepEquals, t.expected)
	}
}

func (l *lkenvTestSuite) TestCopyStringNoPanic(c *C) {
	// too long, string should get concatenate
	b := make([]byte, 5)
	defer lkenv.CopyString(b, "12345")
	c.Assert(recover(), IsNil)
	defer lkenv.CopyString(b, "123456")
	c.Assert(recover(), IsNil)
}

func (l *lkenvTestSuite) TestSaveNoBak(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	env.Set("snap_mode", "trying")
	env.Set("snap_kernel", "kernel-1")
	env.Set("snap_try_kernel", "kernel-2")
	env.Set("snap_core", "core-1")
	env.Set("snap_try_core", "core-2")
	env.Set("snap_gadget", "gadget-1")
	env.Set("snap_try_gadget", "gadget-2")
	env.Set("bootimg_file_name", "boot.img")

	err = env.Save()
	c.Assert(err, IsNil)

	env2 := lkenv.NewEnv(l.envPath)
	err = env2.Load()
	c.Assert(err, IsNil)
	c.Check(env2.Get("snap_mode"), Equals, "trying")
	c.Check(env2.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env2.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env2.Get("snap_core"), Equals, "core-1")
	c.Check(env2.Get("snap_try_core"), Equals, "core-2")
	c.Check(env2.Get("snap_gadget"), Equals, "gadget-1")
	c.Check(env2.Get("snap_try_gadget"), Equals, "gadget-2")
	c.Check(env2.Get("bootimg_file_name"), Equals, "boot.img")
}

func (l *lkenvTestSuite) TestFailedCRC(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPathbak, buf, 0644)
	c.Assert(err, IsNil)
	l.TestFailedCRCNoBak(c)
}

func (l *lkenvTestSuite) TestFailedCRCNoBak(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	err = env.Load()
	c.Assert(err, NotNil)
}

func (l *lkenvTestSuite) TestFailedCRCFallBack(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	env.Set("snap_mode", "trying")
	env.Set("snap_kernel", "kernel-1")
	env.Set("snap_try_kernel", "kernel-2")
	err = env.Save()
	c.Assert(err, IsNil)

	// break main  env file
	err = ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env2 := lkenv.NewEnv(l.envPath)
	err = env2.Load()
	c.Assert(err, IsNil)
	c.Check(env2.Get("snap_mode"), Equals, "trying")
	c.Check(env2.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env2.Get("snap_try_kernel"), Equals, "kernel-2")
}

func (l *lkenvTestSuite) TestGetBootPartition(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Assert(err, IsNil)
	env.ConfigureBootPartitions("boot_a", "boot_b")
	// test no boot partition used
	p, err := env.FindFreeBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	//  set kernel-2 to boot_a partition
	err = env.SetBootPartition("boot_a", "kernel-1")
	c.Assert(err, IsNil)
	//  set kernel-2 to boot_a partition
	err = env.SetBootPartition("boot_b", "kernel-2")
	c.Assert(err, IsNil)

	// 'boot_a' has 'kernel-1' revision
	p, err = env.GetBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// 'boot_b' has 'kernel-2' revision
	p, err = env.GetBootPartition("kernel-2")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
}

func (l *lkenvTestSuite) TestFindFree_Set_Free_BootPartition(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Assert(err, IsNil)
	env.ConfigureBootPartitions("boot_a", "boot_b")
	// test no boot partition used
	p, err := env.FindFreeBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	//  set kernel-2 to boot_a partition
	err = env.SetBootPartition("boot_a", "kernel-2")
	c.Assert(err, IsNil)

	env.Set("snap_kernel", "kernel-2")
	// kernel-2 should now return first part, as it's already there
	p, err = env.FindFreeBootPartition("kernel-2")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// test kernel-1 snapd, it should now offer second partition
	p, err = env.FindFreeBootPartition("kernel-1")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
	err = env.SetBootPartition("boot_b", "kernel-1")
	c.Assert(err, IsNil)
	// set boot kernel-1
	env.Set("snap_kernel", "kernel-1")
	// now kernel-2 should not be protected and boot_a shoild be offered
	p, err = env.FindFreeBootPartition("kernel-3")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	err = env.SetBootPartition("boot_a", "kernel-3")
	c.Assert(err, IsNil)
	// remove kernel
	used, err := env.FreeBootPartition("kernel-3")
	c.Assert(err, IsNil)
	c.Check(used, Equals, true)
	// repeated use should return false and error
	used, err = env.FreeBootPartition("kernel-3")
	c.Assert(err, NotNil)
	c.Check(used, Equals, false)
}

func (l *lkenvTestSuite) TestZippedDataSample(c *C) {
	// test data is generated with gadget build helper tool:
	// $ parts/snap-boot-sel-env/build/lk-boot-env -w test.bin
	//   --snap-mode="trying" --snap-kernel="kernel-1" --snap-try-kernel="kernel-2"
	//   --snap-core="core-1" --snap-try-core="core-2" --reboot-reason=""
	//   --boot-0-part="boot_a" --boot-1-part="boot_b" --boot-0-snap="kernel-1"
	//   --boot-1-snap="kernel-3" --bootimg-file="boot.img"
	// $ cat test.bin | gzip | xxd -i
	gzipedData := []byte{
		0x1f, 0x8b, 0x08, 0x00, 0x95, 0x88, 0x77, 0x5d, 0x00, 0x03, 0xed, 0xd7,
		0xc1, 0x09, 0xc2, 0x40, 0x10, 0x05, 0xd0, 0xa4, 0x20, 0x05, 0x63, 0x07,
		0x96, 0xa0, 0x05, 0x88, 0x91, 0x25, 0x04, 0x35, 0x0b, 0x6b, 0x2e, 0x1e,
		0xac, 0xcb, 0xf6, 0xc4, 0x90, 0x1e, 0x06, 0xd9, 0xf7, 0x2a, 0xf8, 0xc3,
		0x1f, 0x18, 0xe6, 0x74, 0x78, 0xa6, 0xb6, 0x69, 0x9b, 0xb9, 0xbc, 0xc6,
		0x69, 0x68, 0xaa, 0x75, 0xcd, 0x25, 0x6d, 0x76, 0xd1, 0x29, 0xe2, 0x2c,
		0xf3, 0x77, 0xd1, 0x29, 0xe2, 0xdc, 0x52, 0x99, 0xd2, 0xbd, 0xde, 0x0d,
		0x58, 0xe7, 0xaf, 0x78, 0x03, 0x80, 0x5a, 0xf5, 0x39, 0xcf, 0xe7, 0x4b,
		0x74, 0x8a, 0x38, 0xb5, 0xdf, 0xbf, 0xa5, 0xff, 0x3e, 0x3a, 0x45, 0x9c,
		0xb5, 0xff, 0x7d, 0x74, 0x8e, 0x28, 0xbf, 0xfe, 0xb7, 0xe3, 0xa3, 0xe2,
		0x0f, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xf8, 0x17, 0xc7, 0xf7, 0xa7, 0xfb, 0x02, 0x1c, 0xdf, 0x44, 0x21, 0x0c,
		0x3a, 0x00, 0x00}

	// uncompress test data to sample env file
	rawData, err := unpackTestData(gzipedData)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPath, rawData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, rawData, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)
	err = env.Load()
	c.Assert(err, IsNil)
	c.Check(env.Get("snap_mode"), Equals, "trying")
	c.Check(env.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env.Get("snap_core"), Equals, "core-1")
	c.Check(env.Get("snap_try_core"), Equals, "core-2")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("reboot_reason"), Equals, "")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p, err := env.GetBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// test second boot partition is free with label "boot_b"
	p, err = env.FindFreeBootPartition("kernel-2")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
}
