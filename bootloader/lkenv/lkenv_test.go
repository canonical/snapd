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
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/xerrors"
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type lkenvTestSuite struct {
	testutil.BaseTest

	envPath    string
	envPathbak string
}

var _ = Suite(&lkenvTestSuite{})

var lkversions = []lkenv.Version{
	lkenv.V1,
	lkenv.V2Run,
	lkenv.V2Recovery,
}

func (l *lkenvTestSuite) SetUpTest(c *C) {
	l.BaseTest.SetUpTest(c)
	l.envPath = filepath.Join(c.MkDir(), "snapbootsel.bin")
	l.envPathbak = l.envPath + "bak"
}

// unpack test data packed with gzip
func unpackTestData(data []byte) ([]byte, error) {
	b := bytes.NewBuffer(data)
	r := mylog.Check2(gzip.NewReader(b))

	var env bytes.Buffer
	_ = mylog.Check2(env.ReadFrom(r))

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
		// first \0 is the cutoff
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
				mylog.Check(os.WriteFile(testFileBackup, buf, 0644))
				c.Assert(err, IsNil, comment)
			}

			buf := make([]byte, 4096)
			mylog.Check(os.WriteFile(testFile, buf, 0644))
			c.Assert(err, IsNil, comment)

			env := lkenv.NewEnv(testFile, "", t.version)
			c.Check(env, NotNil, comment)

			for k, v := range t.keyValuePairs {
				env.Set(k, v)
			}
			mylog.Check(env.Save())
			c.Assert(err, IsNil, comment)

			env2 := lkenv.NewEnv(testFile, "", t.version)
			mylog.Check(env2.Load())
			c.Assert(err, IsNil, comment)

			for k, v := range t.keyValuePairs {
				c.Check(env2.Get(k), Equals, v, comment)
			}

			// check the backup too
			if makeBackup {
				env3 := lkenv.NewEnv(testFileBackup, "", t.version)
				mylog.Check(env3.Load())
				c.Assert(err, IsNil, comment)

				for k, v := range t.keyValuePairs {
					c.Check(env3.Get(k), Equals, v, comment)
				}

				// corrupt the main file and then try to load it - we should
				// automatically fallback to the backup file since the backup
				// file will not be corrupt
				buf := make([]byte, 4096)
				f := mylog.Check2(os.OpenFile(testFile, os.O_WRONLY, 0644))

				_ = mylog.Check2(io.Copy(f, bytes.NewBuffer(buf)))
				c.Assert(err, IsNil, comment)

				env4 := lkenv.NewEnv(testFile, "", t.version)
				mylog.Check(env4.Load())
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
		mylog.Check(binary.Write(buf, binary.LittleEndian, rawStruct))


		// calculate the expected checksum but don't put it into the object when
		// we write it out so that the checksum is invalid
		expCrc32 := crc32.ChecksumIEEE(buf.Bytes()[:ss-4])
		mylog.Check(os.WriteFile(testFile, buf.Bytes(), 0644))


		// now try importing the file with LoadEnv()
		env := lkenv.NewEnv(testFile, "", version)
		c.Assert(env, NotNil)
		mylog.Check(env.LoadEnv(testFile))
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
		mylog.
			// make empty files for Save() to overwrite
			Check(os.WriteFile(testFile, nil, 0644))

		mylog.Check(os.WriteFile(testFile+"bak", nil, 0644))

		env := lkenv.NewEnv(testFile, "", version)
		c.Assert(env, NotNil)
		mylog.Check(env.Save())


		// make sure both the primary and backup files were written and can be
		// successfully loaded
		env2 := lkenv.NewEnv(testFile, "", version)
		mylog.Check(env2.Load())


		env3 := lkenv.NewEnv(testFile+"bak", "", version)
		mylog.Check(env3.Load())


		// no messages logged
		c.Assert(logbuf.String(), Equals, "")
	}

	// now specify a different backup file location
	for _, version := range lkversions {
		logbuf, restore := logger.MockLogger()
		defer restore()
		testFile := filepath.Join(c.MkDir(), "lk.bin")
		testFileBackup := filepath.Join(c.MkDir(), "lkbackup.bin")
		mylog.Check(os.WriteFile(testFile, nil, 0644))

		mylog.Check(os.WriteFile(testFileBackup, nil, 0644))


		env := lkenv.NewEnv(testFile, testFileBackup, version)
		c.Assert(env, NotNil)
		mylog.Check(env.Save())


		// make sure both the primary and backup files were written and can be
		// successfully loaded
		env2 := lkenv.NewEnv(testFile, "", version)
		mylog.Check(env2.Load())


		env3 := lkenv.NewEnv(testFileBackup, "", version)
		mylog.Check(env3.Load())


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
		mylog.Check(binary.Write(buf, binary.LittleEndian, rawStruct))


		// calculate crc32
		newCrc32 := crc32.ChecksumIEEE(buf.Bytes()[:ss-4])
		// note for efficiency's sake to avoid re-writing the whole structure,
		// we re-write _just_ the crc32 to w as little-endian
		buf.Truncate(ss - 4)
		binary.Write(buf, binary.LittleEndian, &newCrc32)
		mylog.Check(os.WriteFile(testFile, buf.Bytes(), 0644))


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
		mylog.Check(env.LoadEnv(testFile))
		c.Assert(err, ErrorMatches, expErr)
	}
}

func (l *lkenvTestSuite) TestLoadPropagatesErrNotExist(c *C) {
	// make sure that if the env file doesn't exist, the error returned from
	// Load() is os.ErrNotExist, even if it isn't exactly that
	env := lkenv.NewEnv("some-nonsense-file-this-doesnt-exist", "", lkenv.V1)
	c.Check(env, NotNil)
	mylog.Check(env.Load())
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
				mylog.Check(os.WriteFile(testFileBackup, buf, 0644))

			}

			buf := make([]byte, 100000)
			mylog.Check(os.WriteFile(testFile, buf, 0644))


			// create an env for this file and try to load it
			env := lkenv.NewEnv(testFile, "", version)
			c.Check(env, NotNil)
			mylog.Check(env.Load())
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
		mylog.Check(os.WriteFile(l.envPath, buf, 0644))
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
				mylog.Check(
					// for assigning the kernel, we need to also set the
					// snap_kernel, since that is used to detect if we should return
					// an unset variable or not

					env.SetBootPartitionKernel(s1, s2))
				c.Assert(err, IsNil, comment)

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
		mylog.Check(env.InitializeBootPartitions(t.bootMatrixKeys...))
		c.Assert(err, IsNil, comment)

		// before assigning any values to the boot matrix, check that all
		// values we try to assign would go to the first bootPartLabel
		for _, bootPartValue := range t.bootMatrixKeys {
			// we haven't assigned anything yet, so all values should get mapped
			// to the first boot image partition
			bootPartFound := mylog.Check2(findFunc(bootPartValue))
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
			bootPartFound := mylog.Check2(findFunc(bootPartValue))
			c.Assert(err, IsNil, comment)
			c.Assert(bootPartFound, Equals, bootPart, comment)
			mylog.Check(setFunc(bootPart, bootPartValue))
			c.Assert(err, IsNil, comment)

			// now check that it has the right value
			val := mylog.Check2(getFunc(bootPartValue))
			c.Assert(err, IsNil, comment)
			c.Assert(val, Equals, bootPart, comment)

			// double-check that finding a free slot for this value returns the
			// existing slot - this logic specifically is important for uc16 and
			// uc18 where during seeding we will end up extracting a kernel to
			// the already extracted slot (since the kernel will already have
			// been extracted during image build time)
			bootPartFound2 := mylog.Check2(findFunc(bootPartValue))
			c.Assert(err, IsNil, comment)
			c.Assert(bootPartFound2, Equals, bootPart, comment)
		}

		// now check that trying to find a free slot for a new recovery system
		// fails because we are full
		if t.matrixType == "recovery-system" {
			thing := mylog.Check2(findFunc("some-random-value"))
			c.Check(thing, Equals, "")
			c.Assert(err, ErrorMatches, "cannot find free boot image partition", comment)
		}

		// test that removing the last one works
		lastIndex := len(t.bootMatrixValues) - 1
		lastValue := t.bootMatrixValues[lastIndex]
		lastKey := t.bootMatrixKeys[lastIndex]
		mylog.Check(deleteFunc(lastValue))
		c.Assert(err, IsNil, comment)
		mylog.Check(

			// trying to delete again will fail since it won't exist
			deleteFunc(lastValue))
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find %q in boot image partitions", lastValue), comment)

		// trying to find it will return the last slot
		slot := mylog.Check2(findFunc(lastValue))
		c.Assert(err, IsNil, comment)
		c.Assert(slot, Equals, lastKey, comment)
	}
}

func (l *lkenvTestSuite) TestV1NoRecoverySystemSupport(c *C) {
	env := lkenv.NewEnv(l.envPath, "", lkenv.V1)
	c.Assert(env, NotNil)

	_ := mylog.Check2(env.FindFreeRecoverySystemBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")
	mylog.Check(env.SetBootPartitionRecoverySystem("blah", "blah"))
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")

	_ = mylog.Check2(env.GetRecoverySystemBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")
	mylog.Check(env.RemoveRecoverySystemFromBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v1 lkenv has no boot image partition recovery system matrix")
}

func (l *lkenvTestSuite) TestV2RunNoRecoverySystemSupport(c *C) {
	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Run)
	c.Assert(env, NotNil)

	_ := mylog.Check2(env.FindFreeRecoverySystemBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")
	mylog.Check(env.SetBootPartitionRecoverySystem("blah", "blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")

	_ = mylog.Check2(env.GetRecoverySystemBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")
	mylog.Check(env.RemoveRecoverySystemFromBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 run lkenv has no boot image partition recovery system matrix")
}

func (l *lkenvTestSuite) TestV2RecoveryNoKernelSupport(c *C) {
	env := lkenv.NewEnv(l.envPath, "", lkenv.V2Recovery)
	c.Assert(env, NotNil)

	_ := mylog.Check2(env.FindFreeKernelBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")
	mylog.Check(env.SetBootPartitionKernel("blah", "blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")

	_ = mylog.Check2(env.GetKernelBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")
	mylog.Check(env.RemoveKernelFromBootPartition("blah"))
	c.Assert(err, ErrorMatches, "internal error: v2 recovery lkenv has no boot image partition kernel matrix")
}

func (l *lkenvTestSuite) TestZippedDataSample(c *C) {
	// TODO: add binary data test for v2 structures generated with gadget build
	// tool when it has been updated for v2

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
		0x3a, 0x00, 0x00,
	}

	// uncompress test data to sample env file
	rawData := mylog.Check2(unpackTestData(gzipedData))

	mylog.Check(os.WriteFile(l.envPath, rawData, 0644))

	mylog.Check(os.WriteFile(l.envPathbak, rawData, 0644))


	env := lkenv.NewEnv(l.envPath, "", lkenv.V1)
	c.Check(env, NotNil)
	mylog.Check(env.Load())

	c.Check(env.Get("snap_mode"), Equals, boot.TryingStatus)
	c.Check(env.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env.Get("snap_core"), Equals, "core-1")
	c.Check(env.Get("snap_try_core"), Equals, "core-2")
	c.Check(env.Get("bootimg_file_name"), Equals, "boot.img")
	c.Check(env.Get("reboot_reason"), Equals, "")
	// first partition should be with label 'boot_a' and 'kernel-1' revision
	p := mylog.Check2(env.GetKernelBootPartition("kernel-1"))
	c.Check(p, Equals, "boot_a")

	// test second boot partition is free with label "boot_b"
	p = mylog.Check2(env.FindFreeKernelBootPartition("kernel-2"))
	c.Check(p, Equals, "boot_b")

}
