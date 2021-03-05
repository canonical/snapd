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
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/xerrors"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type lkenvTestSuite struct {
	envPath    string
	envPathbak string
}

var _ = Suite(&lkenvTestSuite{})

var (
	lkversions = []lkenv.Version{
		lkenv.V1,
		lkenv.V2Run,
		lkenv.V2Recovery,
	}
)

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

func (l *lkenvTestSuite) TestGetBootImageName(c *C) {
	for _, version := range lkversions {
		for _, setValue := range []bool{true, false} {
			env := lkenv.NewEnv(l.envPath, "", version)
			c.Check(env, NotNil)

			if setValue {
				env.Set("bootimg_file_name", "some-boot-image-name")
			}

			name := env.GetBootImageName()

			if setValue {
				c.Assert(name, Equals, "some-boot-image-name")
			} else {
				c.Assert(name, Equals, "boot.img")
			}
		}
	}
}

func (l *lkenvTestSuite) TestGetDtboImageName(c *C) {
	for _, version := range lkversions {
		for _, setValue := range []bool{true, false} {
			env := lkenv.NewEnv(l.envPath, "", version)
			c.Check(env, NotNil)

			if setValue {
				env.Set("dtboimg_file_name", "some-dtbo-image-name")
			}

			name := env.GetDtboImageName()
			if version == lkenv.V1 {
				c.Assert(name, Equals, "")
			} else {
				if setValue {
					c.Assert(name, Equals, "some-dtbo-image-name")
				} else {
					c.Assert(name, Equals, "")
				}
			}
		}
	}
}

func (l *lkenvTestSuite) TestSet(c *C) {
	tt := []struct {
		version lkenv.Version
		key     string
		val     string
	}{
		{
			lkenv.V1,
			"snap_mode",
			boot.TryStatus,
		},
		{
			lkenv.V2Run,
			"kernel_status",
			boot.TryingStatus,
		},
		{
			lkenv.V2Recovery,
			"snapd_recovery_mode",
			"recover",
		},
	}
	for _, t := range tt {
		env := lkenv.NewEnv(l.envPath, "", t.version)
		c.Check(env, NotNil)
		env.Set(t.key, t.val)
		c.Check(env.Get(t.key), Equals, t.val)
	}
}

func (l *lkenvTestSuite) TestSave(c *C) {
	tt := []struct {
		version       lkenv.Version
		keyValuePairs map[string]string
		comment       string
	}{
		{
			lkenv.V1,
			map[string]string{
				"snap_mode":         boot.TryingStatus,
				"snap_kernel":       "kernel-1",
				"snap_try_kernel":   "kernel-2",
				"snap_core":         "core-1",
				"snap_try_core":     "core-2",
				"snap_gadget":       "gadget-1",
				"snap_try_gadget":   "gadget-2",
				"bootimg_file_name": "boot.img",
			},
			"lkenv v1",
		},
		{
			lkenv.V2Run,
			map[string]string{
				"kernel_status":     boot.TryStatus,
				"snap_kernel":       "kernel-1",
				"snap_try_kernel":   "kernel-2",
				"snap_gadget":       "gadget-1",
				"snap_try_gadget":   "gadget-2",
				"bootimg_file_name": "boot.img",
				"dtboimg_file_name": "dtbo.img",
			},
			"lkenv v2 run",
		},
		{
			lkenv.V2Recovery,
			map[string]string{
				"snapd_recovery_mode":    "recover",
				"snapd_recovery_system":  "11192020",
				"bootimg_file_name":      "boot.img",
				"try_recovery_system":    "1234",
				"recovery_system_status": "tried",
				"dtboimg_file_name":      "dtbo.img",
			},
			"lkenv v2 recovery",
		},
	}
	for _, t := range tt {
		for _, makeBackup := range []bool{true, false} {
			var comment CommentInterface
			if makeBackup {
				comment = Commentf("testcase %s with backup", t.comment)
			} else {
				comment = Commentf("testcase %s without backup", t.comment)
			}

			loggerBuf, restore := logger.MockLogger()
			defer restore()

			// make unique files per test case
			testFile := filepath.Join(c.MkDir(), "lk.bin")
			testFileBackup := testFile + "bak"
			if makeBackup {
				// create the backup file too
				buf := make([]byte, 4096)
				err := ioutil.WriteFile(testFileBackup, buf, 0644)
				c.Assert(err, IsNil, comment)
			}

			buf := make([]byte, 4096)
			err := ioutil.WriteFile(testFile, buf, 0644)
			c.Assert(err, IsNil, comment)

			env := lkenv.NewEnv(testFile, "", t.version)
			c.Check(env, NotNil, comment)

			for k, v := range t.keyValuePairs {
				env.Set(k, v)
			}

			err = env.Save()
			c.Assert(err, IsNil, comment)

			env2 := lkenv.NewEnv(testFile, "", t.version)
			err = env2.Load()
			c.Assert(err, IsNil, comment)

			for k, v := range t.keyValuePairs {
				c.Check(env2.Get(k), Equals, v, comment)
			}

			// check the backup too
			if makeBackup {
				env3 := lkenv.NewEnv(testFileBackup, "", t.version)
				err := env3.Load()
				c.Assert(err, IsNil, comment)

				for k, v := range t.keyValuePairs {
					c.Check(env3.Get(k), Equals, v, comment)
				}

				// corrupt the main file and then try to load it - we should
				// automatically fallback to the backup file since the backup
				// file will not be corrupt
				buf := make([]byte, 4096)
				f, err := os.OpenFile(testFile, os.O_WRONLY, 0644)
				c.Assert(err, IsNil)
				_, err = io.Copy(f, bytes.NewBuffer(buf))
				c.Assert(err, IsNil, comment)

				env4 := lkenv.NewEnv(testFile, "", t.version)
				err = env4.Load()
				c.Assert(err, IsNil, comment)

				for k, v := range t.keyValuePairs {
					c.Check(env4.Get(k), Equals, v, comment)
				}

				// we should have also had a logged message about being unable
				// to load the main file
				c.Assert(loggerBuf.String(), testutil.Contains, fmt.Sprintf("cannot load primary bootloader environment: cannot validate %s:", testFile))
			}
		}
	}
}

func (l *lkenvTestSuite) TestLoadValidatesCRC32(c *C) {
	for _, version := range lkversions {
		testFile := filepath.Join(c.MkDir(), "lk.bin")

		// make an out of band lkenv object and set the wrong signature to be
		// able to export it to a file
		var rawStruct interface{}
		switch version {
		case lkenv.V1:
			rawStruct = lkenv.SnapBootSelect_v1{
				Version:   version.Number(),
				Signature: version.Signature(),
			}
		case lkenv.V2Run:
			rawStruct = lkenv.SnapBootSelect_v2_run{
				Version:   version.Number(),
				Signature: version.Signature(),
			}
		case lkenv.V2Recovery:
			rawStruct = lkenv.SnapBootSelect_v2_recovery{
				Version:   version.Number(),
				Signature: version.Signature(),
			}
		}

		buf := bytes.NewBuffer(nil)
		ss := binary.Size(rawStruct)
		buf.Grow(ss)
		err := binary.Write(buf, binary.LittleEndian, rawStruct)
		c.Assert(err, IsNil)

		// calculate the expected checksum but don't put it into the object when
		// we write it out so that the checksum is invalid
		expCrc32 := crc32.ChecksumIEEE(buf.Bytes()[:ss-4])

		err = ioutil.WriteFile(testFile, buf.Bytes(), 0644)
		c.Assert(err, IsNil)

		// now try importing the file with LoadEnv()
		env := lkenv.NewEnv(testFile, "", version)
		c.Assert(env, NotNil)

		err = env.LoadEnv(testFile)
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot validate %s: expected checksum 0x%X, got 0x%X", testFile, expCrc32, 0))
	}

}

func (l *lkenvTestSuite) TestNewBackupFileLocation(c *C) {
	// creating with the second argument as the empty string falls back to
	// the main path + "bak"
	for _, version := range lkversions {
		logbuf, restore := logger.MockLogger()
		defer restore()

		testFile := filepath.Join(c.MkDir(), "lk.bin")
		c.Assert(testFile, testutil.FileAbsent)
		c.Assert(testFile+"bak", testutil.FileAbsent)
		// make empty files for Save() to overwrite
		err := ioutil.WriteFile(testFile, nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(testFile+"bak", nil, 0644)
		c.Assert(err, IsNil)
		env := lkenv.NewEnv(testFile, "", version)
		c.Assert(env, NotNil)
		err = env.Save()
		c.Assert(err, IsNil)

		// make sure both the primary and backup files were written and can be
		// successfully loaded
		env2 := lkenv.NewEnv(testFile, "", version)
		err = env2.Load()
		c.Assert(err, IsNil)

		env3 := lkenv.NewEnv(testFile+"bak", "", version)
		err = env3.Load()
		c.Assert(err, IsNil)

		// no messages logged
		c.Assert(logbuf.String(), Equals, "")
	}

	// now specify a different backup file location
	for _, version := range lkversions {
		logbuf, restore := logger.MockLogger()
		defer restore()
		testFile := filepath.Join(c.MkDir(), "lk.bin")
		testFileBackup := filepath.Join(c.MkDir(), "lkbackup.bin")
		err := ioutil.WriteFile(testFile, nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(testFileBackup, nil, 0644)
		c.Assert(err, IsNil)

		env := lkenv.NewEnv(testFile, testFileBackup, version)
		c.Assert(env, NotNil)
		err = env.Save()
		c.Assert(err, IsNil)

		// make sure both the primary and backup files were written and can be
		// successfully loaded
		env2 := lkenv.NewEnv(testFile, "", version)
		err = env2.Load()
		c.Assert(err, IsNil)

		env3 := lkenv.NewEnv(testFileBackup, "", version)
		err = env3.Load()
		c.Assert(err, IsNil)

		// no "bak" files present
		c.Assert(testFile+"bak", testutil.FileAbsent)
		c.Assert(testFileBackup+"bak", testutil.FileAbsent)

		// no messages logged
		c.Assert(logbuf.String(), Equals, "")
	}
}

func (l *lkenvTestSuite) TestLoadValidatesVersionSignatureConsistency(c *C) {

	tt := []struct {
		version          lkenv.Version
		binVersion       uint32
		binSignature     uint32
		validateFailMode string
	}{
		{
			lkenv.V1,
			lkenv.V2Recovery.Number(),
			lkenv.V1.Signature(),
			"version",
		},
		{
			lkenv.V1,
			lkenv.V1.Number(),
			lkenv.V2Recovery.Signature(),
			"signature",
		},
		{
			lkenv.V2Run,
			lkenv.V1.Number(),
			lkenv.V2Run.Signature(),
			"version",
		},
		{
			lkenv.V2Run,
			lkenv.V2Run.Number(),
			lkenv.V2Recovery.Signature(),
			"signature",
		},
		{
			lkenv.V2Recovery,
			lkenv.V1.Number(),
			lkenv.V2Recovery.Signature(),
			"version",
		},
		{
			lkenv.V2Recovery,
			lkenv.V2Recovery.Number(),
			lkenv.V2Run.Signature(),
			"signature",
		},
	}

	for _, t := range tt {
		testFile := filepath.Join(c.MkDir(), "lk.bin")

		// make an out of band lkenv object and set the wrong signature to be
		// able to export it to a file
		var rawStruct interface{}
		switch t.version {
		case lkenv.V1:
			rawStruct = lkenv.SnapBootSelect_v1{
				Version:   t.binVersion,
				Signature: t.binSignature,
			}
		case lkenv.V2Run:
			rawStruct = lkenv.SnapBootSelect_v2_run{
				Version:   t.binVersion,
				Signature: t.binSignature,
			}
		case lkenv.V2Recovery:
			rawStruct = lkenv.SnapBootSelect_v2_recovery{
				Version:   t.binVersion,
				Signature: t.binSignature,
			}
		}

		buf := bytes.NewBuffer(nil)
		ss := binary.Size(rawStruct)
		buf.Grow(ss)
		err := binary.Write(buf, binary.LittleEndian, rawStruct)
		c.Assert(err, IsNil)

		// calculate crc32
		newCrc32 := crc32.ChecksumIEEE(buf.Bytes()[:ss-4])
		// note for efficiency's sake to avoid re-writing the whole structure,
		// we re-write _just_ the crc32 to w as little-endian
		buf.Truncate(ss - 4)
		binary.Write(buf, binary.LittleEndian, &newCrc32)

		err = ioutil.WriteFile(testFile, buf.Bytes(), 0644)
		c.Assert(err, IsNil)

		// now try importing the file with LoadEnv()
		env := lkenv.NewEnv(testFile, "", t.version)
		c.Assert(env, NotNil)

		var expNum, gotNum uint32
		switch t.validateFailMode {
		case "signature":
			expNum = t.version.Signature()
			gotNum = t.binSignature
		case "version":
			expNum = t.version.Number()
			gotNum = t.binVersion
		}
		expErr := fmt.Sprintf(
			"cannot validate %s: expected %s 0x%X, got 0x%X",
			testFile,
			t.validateFailMode,
			expNum,
			gotNum,
		)

		err = env.LoadEnv(testFile)
		c.Assert(err, ErrorMatches, expErr)
	}
}

func (l *lkenvTestSuite) TestLoadPropagatesErrNotExist(c *C) {
	// make sure that if the env file doesn't exist, the error returned from
	// Load() is os.ErrNotExist, even if it isn't exactly that
	env := lkenv.NewEnv("some-nonsense-file-this-doesnt-exist", "", lkenv.V1)
	c.Check(env, NotNil)

	err := env.Load()
	c.Assert(xerrors.Is(err, os.ErrNotExist), Equals, true, Commentf("err is %+v", err))
	c.Assert(err, ErrorMatches, "cannot open LK env file: open some-nonsense-file-this-doesnt-existbak: no such file or directory")
}

func (l *lkenvTestSuite) TestLoad(c *C) {
	for _, version := range lkversions {
		for _, makeBackup := range []bool{true, false} {
			loggerBuf, restore := logger.MockLogger()
			defer restore()
			// make unique files per test case
			testFile := filepath.Join(c.MkDir(), "lk.bin")
			testFileBackup := testFile + "bak"
			if makeBackup {
				buf := make([]byte, 100000)
				err := ioutil.WriteFile(testFileBackup, buf, 0644)
				c.Assert(err, IsNil)
			}

			buf := make([]byte, 100000)
			err := ioutil.WriteFile(testFile, buf, 0644)
			c.Assert(err, IsNil)

			// create an env for this file and try to load it
			env := lkenv.NewEnv(testFile, "", version)
			c.Check(env, NotNil)

			err = env.Load()
			// possible error messages could be "cannot open LK env file: ..."
			// or "cannot valid <file>: ..."
			if makeBackup {
				// here we will read the backup file which exists but like the
				// primary file is corrupted
				c.Assert(err, ErrorMatches, fmt.Sprintf("cannot validate %s: expected version 0x%X, got 0x0", testFileBackup, version.Number()))
			} else {
				// here we fail to read the normal file, and automatically try
				// to read the backup, but fail because it doesn't exist
				c.Assert(err, ErrorMatches, fmt.Sprintf("cannot open LK env file: open %s: no such file or directory", testFileBackup))
			}

			c.Assert(loggerBuf.String(), testutil.Contains, fmt.Sprintf("cannot load primary bootloader environment: cannot validate %s:", testFile))
			c.Assert(loggerBuf.String(), testutil.Contains, "attempting to load backup bootloader environment")
		}
	}
}

func (l *lkenvTestSuite) TestGetAndSetAndFindBootPartition(c *C) {
	tt := []struct {
		version lkenv.Version
		// use slices instead of a map since we need a consistent ordering
		bootMatrixKeys   []string
		bootMatrixValues []string
		matrixType       string
		comment          string
	}{
		{
			lkenv.V1,
			[]string{
				"boot_a",
				"boot_b",
			},
			[]string{
				"kernel-1",
				"kernel-2",
			},
			"kernel",
			"v1",
		},
		{
			lkenv.V2Run,
			[]string{
				"boot_a",
				"boot_b",
			},
			[]string{
				"kernel-1",
				"kernel-2",
			},
			"kernel",
			"v2 run",
		},
		{
			lkenv.V2Recovery,
			[]string{
				"boot_recovery_1",
			},
			[]string{
				"20201123",
			},
			"recovery-system",
			"v2 recovery 1 slot",
		},
		{
			lkenv.V2Recovery,
			[]string{
				"boot_recovery_1",
				"boot_recovery_2",
			},
			[]string{
				"20201123",
				"20201124",
			},
			"recovery-system",
			"v2 recovery 2 slots",
		},
		{
			lkenv.V2Recovery,
			[]string{
				"boot_recovery_1",
				"boot_recovery_2",
				"boot_recovery_3",
			},
			[]string{
				"20201123",
				"20201124",
				"20201125",
			},
			"recovery-system",
			"v2 recovery 3 slots",
		},
		{
			lkenv.V2Recovery,
			[]string{
				"boot_recovery_1",
				"boot_recovery_2",
				"boot_recovery_3",
				"boot_recovery_4",
				"boot_recovery_5",
				"boot_recovery_6",
				"boot_recovery_7",
				"boot_recovery_8",
				"boot_recovery_9",
				"boot_recovery_10",
			},
			[]string{
				"20201123",
				"20201124",
				"20201125",
				"20201126",
				"20201127",
				"20201128",
				"20201129",
				"20201130",
				"20201131",
				"20201132",
			},
			"recovery-system",
			"v2 recovery max slots",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		// make sure the key and values are the same length for test case
		// consistency check
		c.Assert(t.bootMatrixKeys, HasLen, len(t.bootMatrixValues), comment)

		buf := make([]byte, 4096)
		err := ioutil.WriteFile(l.envPath, buf, 0644)
		c.Assert(err, IsNil, comment)

		env := lkenv.NewEnv(l.envPath, "", t.version)
		c.Assert(env, Not(IsNil), comment)

		var findFunc func(string) (string, error)
		var setFunc func(string, string) error
		var getFunc func(string) (string, error)
		var deleteFunc func(string) error
		switch t.matrixType {
		case "recovery-system":
			findFunc = func(s string) (string, error) { return env.FindFreeRecoverySystemBootPartition(s) }
			setFunc = func(s1, s2 string) error { return env.SetBootPartitionRecoverySystem(s1, s2) }
			getFunc = func(s1 string) (string, error) { return env.GetRecoverySystemBootPartition(s1) }
			deleteFunc = func(s1 string) error { return env.RemoveRecoverySystemFromBootPartition(s1) }
		case "kernel":
			findFunc = func(s string) (string, error) { return env.FindFreeKernelBootPartition(s) }
			setFunc = func(s1, s2 string) error {
				// for assigning the kernel, we need to also set the
				// snap_kernel, since that is used to detect if we should return
				// an unset variable or not

				err := env.SetBootPartitionKernel(s1, s2)
				c.Assert(err, IsNil, comment)
				if err != nil {
					return err
				}
				if env.Get("snap_kernel") == "" {
					// only set it the first time so that the delete logic test
					// works and we only set the first kernel to be snap_kernel
					env.Set("snap_kernel", s2)
				}
				return nil
			}
			getFunc = func(s1 string) (string, error) { return env.GetKernelBootPartition(s1) }
			deleteFunc = func(s1 string) error { return env.RemoveKernelFromBootPartition(s1) }
		default:
			c.Errorf("unexpected matrix type, test setup broken (%s)", comment)
		}

		err = env.InitializeBootPartitions(t.bootMatrixKeys...)
		c.Assert(err, IsNil, comment)

		// before assigning any values to the boot matrix, check that all
		// values we try to assign would go to the first bootPartLabel
		for _, bootPartValue := range t.bootMatrixKeys {
			// we haven't assigned anything yet, so all values should get mapped
			// to the first boot image partition
			bootPartFound, err := findFunc(bootPartValue)
			c.Assert(err, IsNil, comment)
			c.Assert(bootPartFound, Equals, t.bootMatrixKeys[0], comment)
		}

		// now go and assign them, checking that along the way we are assigning
		// to the next slot
		// iterate over the key list to keep the same order
		for i, bootPart := range t.bootMatrixKeys {
			bootPartValue := t.bootMatrixValues[i]
			// now we will be assigning things, so we should check that the
			// assigned boot image partition matches what we expect
			bootPartFound, err := findFunc(bootPartValue)
			c.Assert(err, IsNil, comment)
			c.Assert(bootPartFound, Equals, bootPart, comment)

			err = setFunc(bootPart, bootPartValue)
			c.Assert(err, IsNil, comment)

			// now check that it has the right value
			val, err := getFunc(bootPartValue)
			c.Assert(err, IsNil, comment)
			c.Assert(val, Equals, bootPart, comment)

			// double-check that finding a free slot for this value returns the
			// existing slot - this logic specifically is important for uc16 and
			// uc18 where during seeding we will end up extracting a kernel to
			// the already extracted slot (since the kernel will already have
			// been extracted during image build time)
			bootPartFound2, err := findFunc(bootPartValue)
			c.Assert(err, IsNil, comment)
			c.Assert(bootPartFound2, Equals, bootPart, comment)
		}

		// now check that trying to find a free slot for a new recovery system
		// fails because we are full
		if t.matrixType == "recovery-system" {
			thing, err := findFunc("some-random-value")
			c.Check(thing, Equals, "")
			c.Assert(err, ErrorMatches, "cannot find free boot image partition", comment)
		}

		// test that removing the last one works
		lastIndex := len(t.bootMatrixValues) - 1
		lastValue := t.bootMatrixValues[lastIndex]
		lastKey := t.bootMatrixKeys[lastIndex]
		err = deleteFunc(lastValue)
		c.Assert(err, IsNil, comment)

		// trying to delete again will fail since it won't exist
		err = deleteFunc(lastValue)
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find %q in boot image partitions", lastValue), comment)

		// trying to find it will return the last slot
		slot, err := findFunc(lastValue)
		c.Assert(err, IsNil, comment)
		c.Assert(slot, Equals, lastKey, comment)
	}
}

func (l *lkenvTestSuite) TestV1NoRecoverySystemSupport(c *C) {
	env := lkenv.NewEnv(l.envPath, "", lkenv.V1)
	c.Assert(env, NotNil)

	_, err := env.FindFreeRecoverySystemBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")

	err = env.SetBootPartitionRecoverySystem("blah", "blah")
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")

	_, err = env.GetRecoverySystemBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")

	err = env.RemoveRecoverySystemFromBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")
}

func (l *lkenvTestSuite) TestV2RunNoRecoverySystemSupport(c *C) {
	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Run)
	c.Assert(env, NotNil)

	_, err := env.FindFreeRecoverySystemBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")

	err = env.SetBootPartitionRecoverySystem("blah", "blah")
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")

	_, err = env.GetRecoverySystemBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")

	err = env.RemoveRecoverySystemFromBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")
}

func (l *lkenvTestSuite) TestV2RecoveryNoKernelSupport(c *C) {
	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Recovery)
	c.Assert(env, NotNil)

	_, err := env.FindFreeKernelBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")

	err = env.SetBootPartitionKernel("blah", "blah")
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")

	_, err = env.GetKernelBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")

	err = env.RemoveKernelFromBootPartition("blah")
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")
}

func (l *lkenvTestSuite) TestZippedDataSampleV1(c *C) {
	// test data is generated with gadget build helper tool:
	// $ parts/snap-boot-sel-env/build/lk-boot-env -w test.bin \
	//   --snap-mode="trying" --snap-kernel="kernel-1" --snap-try-kernel="kernel-2" \
	//   --snap-core="core-1" --snap-try-core="core-2" --reboot-reason="" \
	//   --boot-0-part="boot_a" --boot-1-part="boot_b" --boot-0-snap="kernel-1" \
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

	env := lkenv.NewEnv(l.envPath, "", lkenv.V1)
	c.Check(env, NotNil)
	err = env.Load()
	c.Assert(err, IsNil)
	c.Check(env.Get("snap_mode"), Equals, boot.TryingStatus)
	c.Check(env.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env.Get("snap_core"), Equals, "core-1")
	c.Check(env.Get("snap_try_core"), Equals, "core-2")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("reboot_reason"), Equals, "")
	c.Check(env.Get("dtboimg_file_name"), Equals, "")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p, err := env.GetKernelBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// test second boot partition is free with label "boot_b"
	p, err = env.FindFreeKernelBootPartition("kernel-2")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
	// test dtbo mapping, which is not supported for v1
	p, err = env.FindDtboPartition("boot_a")
	c.Check(p, Equals, "")
	c.Assert(err, NotNil)
}

func (l *lkenvTestSuite) TestZippedDataSampleV2runNoDtbo(c *C) {
	// test data is generated with gadget build helper tool:
	// $ parts/snap-boot-sel-env/build//lk-boot-env --runtime -w test.bin \
	//   --kernel-status="trying" --snap-kernel="kernel-1" --snap-try-kernel="kernel-2" \
	//   --boot-0-part="boot_a" --boot-1-part="boot_b" --boot-0-snap="kernel-1" \
	//   --boot-1-snap="kernel-3" --bootimg-file="boot.img"
	// $ cat test.bin | gzip | xxd -i
	gzipedData := []byte{
		0x1f, 0x8b, 0x08, 0x00, 0x56, 0x31, 0x42, 0x60, 0x00, 0x03, 0xed, 0xd6,
		0xc1, 0x09, 0xc2, 0x40, 0x14, 0x45, 0xd1, 0x71, 0x97, 0x45, 0x16, 0x36,
		0x62, 0x40, 0xed, 0xc0, 0x16, 0x2c, 0x40, 0x0c, 0x0c, 0x12, 0x8c, 0x09,
		0xc4, 0x6c, 0xec, 0xc8, 0xf6, 0xec, 0x40, 0x94, 0xd4, 0xe0, 0x47, 0xe6,
		0x9c, 0x0a, 0xee, 0xf0, 0xe0, 0x33, 0xc7, 0xc3, 0x3d, 0xaf, 0xd3, 0x2a,
		0xcd, 0xd3, 0xa3, 0x1b, 0x2e, 0xa9, 0x58, 0xd7, 0x3c, 0x0d, 0xb9, 0xdf,
		0x6c, 0xa3, 0x3b, 0xa2, 0x2c, 0xef, 0xdf, 0x45, 0x77, 0x00, 0xbf, 0xd7,
		0x8e, 0xe3, 0x7c, 0x3a, 0x47, 0x57, 0xc4, 0x29, 0xfd, 0xfe, 0x7f, 0xf7,
		0x6f, 0xa3, 0x2b, 0xe2, 0x2c, 0xfb, 0xef, 0xa3, 0x3b, 0xa2, 0x7c, 0xf6,
		0x6f, 0xba, 0x5b, 0xc1, 0x3f, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0xe0, 0x5f, 0x54, 0xcf, 0xfa, 0xf5, 0x06, 0x1c,
		0xdf, 0x44, 0x21, 0x0c, 0x37, 0x00, 0x00}

	// uncompress test data to sample env file
	rawData, err := unpackTestData(gzipedData)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPath, rawData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, rawData, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Run)
	c.Check(env, NotNil)
	err = env.Load()
	c.Assert(err, IsNil)
	c.Check(env.Get("kernel_status"), Equals, boot.TryingStatus)
	c.Check(env.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("dtboimg_file_name"), Equals, "")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p, err := env.GetKernelBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// test second boot partition is free with label "boot_b"
	p, err = env.FindFreeKernelBootPartition("kernel-2")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
	// test dtbo mapping
	p, err = env.FindDtboPartition("boot_a")
	c.Check(p, Equals, "")
	c.Assert(err, NotNil)
	p, err = env.FindDtboPartition("boot_b")
	c.Check(p, Equals, "")
	c.Assert(err, NotNil)
}

func (l *lkenvTestSuite) TestZippedDataSampleV2run(c *C) {
	// test data is generated with gadget build helper tool:
	// $ parts/snap-boot-sel-env/build//lk-boot-env --runtime -w test.bin \
	//   --kernel-status="trying" --snap-kernel="kernel-1" --snap-try-kernel="kernel-2" \
	//   --boot-0-part="boot_a" --boot-1-part="boot_b" --boot-0-snap="kernel-1" \
	//   --boot-1-snap="kernel-3" --bootimg-file="boot.img" --dtbo-0-part="dtbo_a" \\
	//   --dtbo-1-part="dtbo_b" --dtbo-0-boot="boot_a" --dtbo-1-boot="boot_b" \
	//   --dtboimg-file="dtbo.img
	// $ cat test.bin | gzip | xxd -i
	gzipedData := []byte{
		0x1f, 0x8b, 0x08, 0x00, 0xc7, 0x2f, 0x42, 0x60, 0x00, 0x03, 0xed, 0xd6,
		0xd1, 0x09, 0x82, 0x00, 0x14, 0x05, 0x50, 0xdb, 0xa0, 0xef, 0x76, 0x28,
		0xa8, 0x36, 0x68, 0x84, 0x1a, 0x20, 0x12, 0x25, 0xc4, 0x52, 0x30, 0x7f,
		0x1a, 0xb0, 0xbd, 0x42, 0x71, 0x86, 0x1e, 0xf1, 0xce, 0x99, 0xe0, 0x3e,
		0x2e, 0x3c, 0xee, 0xe5, 0xf4, 0xaa, 0xd7, 0xc5, 0xaa, 0x18, 0x87, 0x77,
		0xd3, 0xdd, 0x8b, 0xb4, 0xda, 0x7a, 0xe8, 0xea, 0xc7, 0x76, 0x1f, 0x9d,
		0x23, 0xca, 0x72, 0xff, 0x21, 0x3a, 0x07, 0xf0, 0x7b, 0x65, 0xdf, 0x8f,
		0xd7, 0x5b, 0x74, 0x8a, 0x38, 0xd9, 0xff, 0xff, 0xdc, 0x7f, 0x19, 0x9d,
		0x22, 0xce, 0xd2, 0xff, 0x31, 0x3a, 0x47, 0x94, 0xa9, 0xff, 0x5d, 0xf3,
		0x4c, 0xbc, 0x00, 0x81, 0xac, 0xaa, 0xb1, 0xec, 0x33, 0xef, 0x9f, 0xec,
		0xfb, 0x6f, 0xee, 0x3f, 0xf1, 0xfe, 0xc9, 0xbe, 0xff, 0xa6, 0xfe, 0xed,
		0x1f, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x4f, 0x9b,
		0x4f, 0x7b, 0xfe, 0x02, 0x1c, 0xdf, 0x44, 0x21, 0x0c, 0x37, 0x00, 0x00}

	// uncompress test data to sample env file
	rawData, err := unpackTestData(gzipedData)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPath, rawData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, rawData, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Run)
	c.Check(env, NotNil)
	err = env.Load()
	c.Assert(err, IsNil)
	c.Check(env.Get("kernel_status"), Equals, boot.TryingStatus)
	c.Check(env.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("dtboimg_file_name"), Equals, "dtbo.img")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p, err := env.GetKernelBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// test second boot partition is free with label "boot_b"
	p, err = env.FindFreeKernelBootPartition("kernel-2")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
	// test dtbo mapping
	p, err = env.FindDtboPartition("boot_a")
	c.Check(p, Equals, "dtbo_a")
	c.Assert(err, IsNil)
	p, err = env.FindDtboPartition("boot_b")
	c.Check(p, Equals, "dtbo_b")
	c.Assert(err, IsNil)
}

func (l *lkenvTestSuite) TestZippedDataSampleV2recoveryNoDtbo(c *C) {
	// test data is generated with gadget build helper tool:
	// $ parts/snap-boot-sel-env/build//./lk-boot-env --recovery -w test.bin \
	//   --revovery-mode="recover" --revovery-system="05032021" \
	//   --recovery-0-part="boot_ra" --recovery-1-part="boot_rb" \
	//   --recovery-0-snap="kernel-1" --recovery-1-snap="kernel-3" \
	//   --bootimg-file="boot.img"
	// $ cat test.bin | gzip | xxd -i
	gzipedData := []byte{
		0x1f, 0x8b, 0x08, 0x00, 0xe8, 0x3c, 0x42, 0x60, 0x00, 0x03, 0xed, 0xd8,
		0xcb, 0x09, 0xc2, 0x40, 0x18, 0x85, 0xd1, 0xa4, 0x03, 0x1b, 0x51, 0xc6,
		0x04, 0x1b, 0xd1, 0x02, 0xc4, 0x84, 0x41, 0xc4, 0xc7, 0xc0, 0x28, 0xe9,
		0xc7, 0x22, 0xdd, 0x8b, 0x60, 0x01, 0x6e, 0xe4, 0x5f, 0xe4, 0x9c, 0x0a,
		0x3e, 0xb8, 0xbb, 0xbb, 0xdb, 0xde, 0xf3, 0xa2, 0x69, 0x9b, 0x9a, 0xc7,
		0x32, 0xe5, 0xda, 0xcc, 0x55, 0xda, 0xa4, 0xbe, 0x4b, 0xdd, 0x3a, 0xba,
		0x23, 0xca, 0x50, 0xca, 0x63, 0x5f, 0x0f, 0xd1, 0x19, 0x61, 0xce, 0xb9,
		0xde, 0xf2, 0x65, 0x39, 0xf3, 0xfd, 0x87, 0xe8, 0x8c, 0x30, 0xdf, 0xfd,
		0xfb, 0xe8, 0x0e, 0x00, 0x00, 0x00, 0x80, 0x7f, 0xf9, 0xfc, 0x3f, 0xab,
		0xd3, 0xf5, 0x18, 0xdd, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xf0, 0xab, 0xa9, 0x7d, 0xbe, 0xde, 0x1c, 0xdf, 0x44, 0x21,
		0x0c, 0x3f, 0x00, 0x00}

	// uncompress test data to sample env file
	rawData, err := unpackTestData(gzipedData)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPath, rawData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, rawData, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Recovery)
	c.Check(env, NotNil)
	err = env.Load()
	c.Assert(err, IsNil)
	c.Check(env.Get("snapd_recovery_mode"), Equals, "recover")
	c.Check(env.Get("snapd_recovery_system"), Equals, "05032021")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("dtboimg_file_name"), Equals, "")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p, err := env.GetRecoverySystemBootPartition("kernel-1")
	c.Check(p, Equals, "boot_ra")
	c.Assert(err, IsNil)
	// test second boot partition is free with label "boot_b"
	p, err = env.FindFreeRecoverySystemBootPartition("kernel-3")
	c.Check(p, Equals, "boot_rb")
	c.Assert(err, IsNil)
	// test dtbo mapping
	p, err = env.FindDtboPartition("boot_ra")
	c.Check(p, Equals, "")
	c.Assert(err, NotNil)
	p, err = env.FindDtboPartition("boot_rb")
	c.Check(p, Equals, "")
	c.Assert(err, NotNil)
}

func (l *lkenvTestSuite) TestZippedDataSampleV2recovery(c *C) {
	// test data is generated with gadget build helper tool:
	// $ parts/snap-boot-sel-env/build//./lk-boot-env --recovery -w test.bin \
	//   --revovery-mode="recover" --revovery-system="05032021" \
	//   --recovery-0-part="boot_ra" --recovery-1-part="boot_rb" \
	//   --recovery-0-snap="kernel-1" --recovery-1-snap="kernel-3" \
	//   --bootimg-file="boot.img" --dtbo-0-part="dtbo_ra" --dtbo-1-part="dtbo_rb" \
	//   --dtbo-0-boot="boot_ra" --dtbo-1-boot="boot_rb" --dtboimg-file="dtbo.img"
	// $ cat test.bin | gzip | xxd -i
	gzipedData := []byte{
		0x1f, 0x8b, 0x08, 0x00, 0xc9, 0x3d, 0x42, 0x60, 0x00, 0x03, 0xed, 0xd8,
		0xd1, 0x09, 0xc2, 0x40, 0x14, 0x04, 0xc0, 0xd8, 0x81, 0x8d, 0x28, 0x31,
		0xc1, 0x46, 0xb4, 0x00, 0x31, 0x7a, 0x88, 0xa8, 0x09, 0x9c, 0xc1, 0xe6,
		0xec, 0x4d, 0x24, 0x68, 0x0b, 0xf2, 0x90, 0x9b, 0xa9, 0x60, 0x61, 0x3f,
		0x16, 0x76, 0xbb, 0xb9, 0xa7, 0x79, 0x35, 0xab, 0x72, 0x3a, 0x0c, 0x8f,
		0x94, 0xab, 0x52, 0xd5, 0xeb, 0xba, 0x6d, 0xea, 0x66, 0x15, 0x9d, 0x23,
		0x4a, 0x37, 0x0c, 0xe3, 0x2e, 0xef, 0xa3, 0x63, 0x84, 0xb9, 0xa4, 0xdc,
		0xa7, 0xeb, 0xa2, 0xf0, 0xfe, 0xbb, 0xe8, 0x18, 0x61, 0xbe, 0xfd, 0xb7,
		0xd1, 0x39, 0x00, 0x00, 0x00, 0x00, 0x7e, 0x65, 0xfa, 0x7f, 0x96, 0xe7,
		0xdb, 0x29, 0x3a, 0x07, 0x44, 0x38, 0x8e, 0xdd, 0x50, 0xf2, 0xff, 0x5d,
		0xfa, 0xff, 0xff, 0xe9, 0xbf, 0xdc, 0xff, 0xbb, 0xf4, 0xff, 0x7f, 0xea,
		0xdf, 0xfe, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfc, 0xaf,
		0xd7, 0xb3, 0x6f, 0xde, 0x1c, 0xdf, 0x44, 0x21, 0x0c, 0x3f, 0x00, 0x00}

	// uncompress test data to sample env file
	rawData, err := unpackTestData(gzipedData)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPath, rawData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, rawData, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Recovery)
	c.Check(env, NotNil)
	err = env.Load()
	c.Assert(err, IsNil)
	c.Check(env.Get("snapd_recovery_mode"), Equals, "recover")
	c.Check(env.Get("snapd_recovery_system"), Equals, "05032021")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("dtboimg_file_name"), Equals, "dtbo.img")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p, err := env.GetRecoverySystemBootPartition("kernel-1")
	c.Check(p, Equals, "boot_ra")
	c.Assert(err, IsNil)
	// test second boot partition is free with label "boot_b"
	p, err = env.FindFreeRecoverySystemBootPartition("kernel-3")
	c.Check(p, Equals, "boot_rb")
	c.Assert(err, IsNil)
	// test dtbo mapping
	p, err = env.FindDtboPartition("boot_ra")
	c.Check(p, Equals, "dtbo_ra")
	c.Assert(err, IsNil)
	p, err = env.FindDtboPartition("boot_rb")
	c.Check(p, Equals, "dtbo_rb")
	c.Assert(err, IsNil)
}
