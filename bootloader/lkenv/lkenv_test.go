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
	gzipedData := []byte{
		0x1f, 0x8b, 0x08, 0x08, 0xf4, 0x7a, 0x4f, 0x5d, 0x02, 0x03, 0x74, 0x65, 0x73, 0x74, 0x2d, 0x65,
		0x6e, 0x76, 0x2e, 0x62, 0x69, 0x6e, 0x00, 0xed, 0xd4, 0xd1, 0x09, 0x83, 0x40, 0x10, 0x04, 0xd0,
		0xb5, 0x20, 0x3f, 0x92, 0x12, 0x6c, 0x21, 0x15, 0xa8, 0x1c, 0x41, 0x12, 0x3c, 0xb8, 0xf8, 0x63,
		0x1d, 0x96, 0x64, 0x1d, 0xe9, 0x25, 0x10, 0xd2, 0x44, 0xd8, 0xf7, 0x3a, 0x18, 0x76, 0x66, 0x6f,
		0xc3, 0xab, 0x74, 0xd1, 0xc5, 0xd6, 0xf6, 0x65, 0xbd, 0x47, 0xcc, 0xb5, 0x95, 0xfe, 0x12, 0x69,
		0x7d, 0xf3, 0x5f, 0xf3, 0xe6, 0x7f, 0x94, 0xb6, 0x96, 0x67, 0xde, 0x06, 0xfc, 0xf2, 0x27, 0x6e,
		0x00, 0xe4, 0x35, 0xd5, 0xba, 0xf5, 0xa3, 0xff, 0x9f, 0xfa, 0xfe, 0x93, 0x1d, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0xf0, 0xdf, 0xc6, 0xe3, 0x7c, 0x7f, 0x00, 0x1c, 0xdf, 0x44, 0x21, 0x14,
		0x28, 0x00, 0x00}

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
	c.Check(env.Get("reboot_reason"), Equals, "")
}
