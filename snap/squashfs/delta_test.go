// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package squashfs_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

type DeltaTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&DeltaTestSuite{})

func (s *DeltaTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	d := c.MkDir()
	dirs.SetRootDir(d)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	err := os.Chdir(d)
	c.Assert(err, IsNil)
}

func (s *DeltaTestSuite) TestSupportedDeltaFormats(c *C) {
	c.Assert(squashfs.SupportedDeltaFormats(
		squashfs.DeltaFormatOpts{WithSnapDeltaFormat: false}), DeepEquals,
		[]string{"xdelta3"})

	c.Assert(squashfs.SupportedDeltaFormats(
		squashfs.DeltaFormatOpts{WithSnapDeltaFormat: true}), DeepEquals,
		[]string{"snap-1-1-xdelta3", "xdelta3"})
}

func (s *DeltaTestSuite) TestCompIdToMksquashfsArgs(c *C) {
	testCases := []struct {
		name          string
		id            uint16
		initialArgs   []string
		expectedArgs  []string
		expectedError string
	}{
		{
			name:          "Gzip compression",
			id:            1,
			initialArgs:   []string{"-all-root"},
			expectedArgs:  []string{"-all-root", "-comp", "gzip"},
			expectedError: "",
		},
		{
			name:          "Lzma compression",
			id:            2,
			initialArgs:   []string{},
			expectedArgs:  []string{"-comp", "lzma"},
			expectedError: "",
		},
		{
			name:          "Lzo compression",
			id:            3,
			initialArgs:   []string{"-no-exports"},
			expectedArgs:  []string{"-no-exports", "-comp", "lzo"},
			expectedError: "",
		},
		{
			name:          "Xz compression",
			id:            4,
			initialArgs:   []string{},
			expectedArgs:  []string{"-comp", "xz"},
			expectedError: "",
		},
		{
			name:          "Lz4 compression",
			id:            5,
			initialArgs:   []string{"-no-xattrs"},
			expectedArgs:  []string{"-no-xattrs", "-comp", "lz4"},
			expectedError: "",
		},
		{
			name:          "Zstd compression",
			id:            6,
			initialArgs:   []string{},
			expectedArgs:  []string{"-comp", "zstd"},
			expectedError: "",
		},
		{
			name:          "Unknown compression",
			id:            99,
			initialArgs:   []string{"-all-root"},
			expectedArgs:  nil,
			expectedError: "unknown compression id: 99",
		},
		{
			name:          "Zero ID (unknown)",
			id:            0,
			initialArgs:   []string{},
			expectedArgs:  nil,
			expectedError: "unknown compression id: 0",
		},
	}

	// Iterate over test cases
	for _, tc := range testCases {
		argsCopy := make([]string, len(tc.initialArgs))
		copy(argsCopy, tc.initialArgs)

		resultArgs, err := squashfs.CompIdToMksquashfsArgs(tc.id, argsCopy)

		if tc.expectedError == "" {
			c.Assert(err, IsNil)
			c.Check(tc.expectedArgs, DeepEquals, resultArgs)
		} else {
			c.Check(err, ErrorMatches, tc.expectedError)
		}
	}
}

func (s *DeltaTestSuite) TestSuperBlockFlagsToMksquashfsArgs(c *C) {
	testCases := []struct {
		name          string
		flags         uint16
		initialArgs   []string
		expectedArgs  []string
		expectedError string
	}{
		{
			name:          "Error on flagCheck",
			flags:         0x0004,
			initialArgs:   []string{},
			expectedArgs:  nil,
			expectedError: "unexpected value in superblock flags",
		},
		{
			name:          "No flags (default)",
			flags:         0,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-duplicates"}, // -no-duplicates is added when flag is *not* set
			expectedError: "",
		},
		{
			name:          "NoFragments flag",
			flags:         0x0010,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-fragments", "-no-duplicates"},
			expectedError: "",
		},
		{
			name:          "NoDuplicates flag (should NOT add -no-duplicates)",
			flags:         0x0040,
			initialArgs:   []string{},
			expectedArgs:  []string{}, // Empty because the only default is suppressed
			expectedError: "",
		},
		{
			name:          "Exports flag",
			flags:         0x0080,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-duplicates", "-exports"},
			expectedError: "",
		},
		{
			name:          "NoXattrs flag",
			flags:         0x0200,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-duplicates", "-no-xattrs"},
			expectedError: "",
		},
		{
			name:          "CompressorOptions flag",
			flags:         0x0400,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-duplicates"},
			expectedError: "compression options was set in target, which is unsupported",
		},
		{
			name:          "Multiple flags (NoFragments, Exports, NoXattrs)",
			flags:         0x0010 | 0x0080 | 0x0200,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-fragments", "-no-duplicates", "-exports", "-no-xattrs"},
			expectedError: "",
		},
		{
			name:          "All flags (NoFragments, NoDuplicates, Exports, NoXattrs)",
			flags:         0x0010 | 0x0040 | 0x0080 | 0x0200,
			initialArgs:   []string{},
			expectedArgs:  []string{"-no-fragments", "-exports", "-no-xattrs"}, // No -no-duplicates
			expectedError: "",
		},
		{
			name:          "With initial args",
			flags:         0x0010,
			initialArgs:   []string{"-existing-arg"},
			expectedArgs:  []string{"-existing-arg", "-no-fragments", "-no-duplicates"},
			expectedError: "",
		},
	}

	for _, tc := range testCases {
		c.Log(tc.name)
		argsCopy := make([]string, len(tc.initialArgs))
		copy(argsCopy, tc.initialArgs)

		resultArgs, err := squashfs.SuperBlockFlagsToMksquashfsArgs(tc.flags, argsCopy)

		if tc.expectedError == "" {
			c.Assert(err, IsNil)
			c.Check(resultArgs, DeepEquals, tc.expectedArgs)
		} else {
			c.Check(err, ErrorMatches, tc.expectedError)
		}
	}
}

func (s *DeltaTestSuite) TestSetupPipesNoPipes(c *C) {
	tempDir, pipePaths, err := squashfs.SetupPipes()
	c.Assert(err, IsNil)
	defer os.RemoveAll(tempDir)

	c.Check(tempDir, Not(HasLen), 0)

	// Check that the directory actually exists
	_, err = os.Stat(tempDir)
	c.Assert(os.IsNotExist(err), Equals, false)
	c.Assert(len(pipePaths), Equals, 0)
}

func (s *DeltaTestSuite) TestSetupPipesMultiplePipes(c *C) {
	pipeNames := []string{"fifo1", "fifo2"}
	tempDir, pipePaths, err := squashfs.SetupPipes(pipeNames...)
	c.Assert(err, IsNil)
	defer os.RemoveAll(tempDir)
	c.Check(tempDir, Not(HasLen), 0)

	c.Check(len(pipePaths), Equals, len(pipeNames))

	for i, name := range pipeNames {
		expectedPath := filepath.Join(tempDir, name)
		c.Check(pipePaths[i], Equals, expectedPath)
		// Check if file exists and is a FIFO pipe
		info, err := os.Stat(pipePaths[i])
		c.Assert(err, IsNil)
		// Check if it's a named pipe (FIFO) using the os.ModeNamedPipe bitmask
		c.Check((info.Mode() & os.ModeNamedPipe), Not(Equals), 0)
	}
}

func (s *DeltaTestSuite) TestSetupPipesFail(c *C) {
	// This name is invalid because it contains a path separator.
	// syscall.Mkfifo will fail trying to create a file in a non-existent subdirectory.
	pipeNames := []string{"good_pipe", "invalid/pipe"}
	tempDir, pipePaths, err := squashfs.SetupPipes(pipeNames...)

	if err == nil {
		// If it did succeed (unexpectedly),cleanup before failing
		defer os.RemoveAll(tempDir)
		c.Assert(err, NotNil)
	}
	// check other return values
	c.Check(tempDir, HasLen, 0)
	c.Check(pipePaths, IsNil)
}

func (s *DeltaTestSuite) createMockSnap(c *C, name string, timestamp uint32, compression uint16, flags uint16) string {
	path := filepath.Join(dirs.GlobalRootDir, name)
	// SquashFS superblock is at least 96 bytes. We need up to offset 26.
	data := make([]byte, 100)

	// squashfs magic number
	binary.LittleEndian.PutUint32(data[0:4], 0x73717368)
	// modification_time @ offset 8 (4 bytes)
	binary.LittleEndian.PutUint32(data[8:12], timestamp)
	// compression_id @ offset 20 (2 bytes)
	binary.LittleEndian.PutUint16(data[20:22], compression)
	// flags @ offset 24 (2 bytes)
	binary.LittleEndian.PutUint16(data[24:26], flags)
	// major @ offset 30 (2 bytes)
	binary.LittleEndian.PutUint16(data[28:30], 4)
	// minor @ offset 32 (2 bytes)
	binary.LittleEndian.PutUint16(data[30:32], 0)

	err := os.WriteFile(path, data, 0644)
	c.Assert(err, IsNil)
	return path
}

// createDeltaFile creates a delta file with a valid SnapDeltaHeader and returns its path.
func (s *DeltaTestSuite) createDeltaFile(c *C, name string, timestamp uint32, compression uint16, flags uint16) string {
	path := filepath.Join(dirs.GlobalRootDir, name)

	// Create a buffer for the header
	buf := new(bytes.Buffer)

	// Serialize fields (Little Endian)
	// Magic (u32)
	binary.Write(buf, binary.LittleEndian, uint32(0x66707173))
	// Format/tools Version (u8, u8)
	binary.Write(buf, binary.LittleEndian, uint8(0x01))
	binary.Write(buf, binary.LittleEndian, uint8(0x01))
	// Delta Tool (u16) - 0x01 for Xdelta3
	binary.Write(buf, binary.LittleEndian, uint16(0x01))
	// Timestamp (u32)
	binary.Write(buf, binary.LittleEndian, timestamp)
	// Compression (u16)
	binary.Write(buf, binary.LittleEndian, compression)
	// Flags (u16)
	binary.Write(buf, binary.LittleEndian, flags)

	// Pad to exactly deltaHeaderSize (32 bytes)
	padding := make([]byte, 32-buf.Len())
	buf.Write(padding)

	err := os.WriteFile(path, buf.Bytes(), 0644)
	c.Assert(err, IsNil)

	return path
}

func (s *DeltaTestSuite) TestGenerateDeltaUnsupportedFormat(c *C) {
	err := squashfs.GenerateDelta(context.Background(), "s", "t", "d", "unsupported-format")
	c.Assert(err, ErrorMatches, `unsupported delta format "unsupported-format"`)
}

func (s *DeltaTestSuite) TestGenerateDeltaPlainSuccess(c *C) {
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			c.Check(cmd, Equals, "/usr/bin/xdelta3")
			c.Check(args, DeepEquals,
				[]string{"-3", "-f", "-e", "-s", "source.snap", "target.snap", "diff.xdelta3"})
			return &exec.Cmd{
				Path: cmd,
				Args: append([]string{cmd}, args...),
			}, nil
		})()

	defer squashfs.MockCmdRun(func(cmd *exec.Cmd) error {
		return nil
	})()

	// Execute
	err := squashfs.GenerateDelta(context.Background(), "source.snap", "target.snap", "diff.xdelta3", "xdelta3")
	c.Assert(err, IsNil)
}

func (s *DeltaTestSuite) TestGenerateDeltaSnapXdelta3Success(c *C) {
	// Setup mock snaps
	// 4 = xz compression, 0x0040 = flagDuplicates
	src := s.createMockSnap(c, "source.snap", 1000, 4, 0x0040)
	dst := s.createMockSnap(c, "target.snap", 2000, 4, 0x0040)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "out.delta")

	// Mock the external commands
	// GenerateDelta for SnapXdelta3Format calls unsquashfs twice and xdelta3 once.
	snapdBinDir := "/usr/bin/"
	unsquashfsPath := filepath.Join(snapdBinDir, "unsquashfs")
	xdelta3Path := filepath.Join(snapdBinDir, "xdelta3")
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			// Execute the builder to get the commands
			cmds, tasks, err := buildWithContext(ctx)
			c.Assert(err, IsNil)
			c.Check(len(cmds), Equals, 3)
			c.Check(len(tasks), Equals, 0)

			// 1. Assert on Unsquashfs Source
			unsquashfsArgs1 := cmds[0].Args
			c.Check(len(unsquashfsArgs1), Equals, 9)
			c.Check(unsquashfsArgs1[0:7], DeepEquals,
				[]string{unsquashfsPath, "-da", "128", "-fr", "128", "-no-progress", "-pf"})
			c.Check(unsquashfsArgs1[7], Matches, "/tmp/snap-delta-.*/src-pipe")
			c.Check(unsquashfsArgs1[8], Matches, "*./source.snap")

			// 2. Assert on Unsquashfs Target
			unsquashfsArgs2 := cmds[1].Args
			c.Check(len(unsquashfsArgs2), Equals, 8)
			c.Check(unsquashfsArgs2[0:6], DeepEquals,
				[]string{unsquashfsPath, "-da", "128", "-fr", "128", "-pf"})
			c.Check(unsquashfsArgs2[6], Matches, "/tmp/snap-delta-.*/trgt-pipe")
			c.Check(unsquashfsArgs2[7], Matches, "*./target.snap")

			// 3. Assert on Xdelta3
			xdelta3Args := cmds[2].Args
			c.Check(len(xdelta3Args), Equals, 8)
			c.Check(xdelta3Args[0:6], DeepEquals, []string{xdelta3Path, "-7", "-e", "-f", "-A", "-s"})
			c.Check(xdelta3Args[6], Matches, "/tmp/snap-delta-.*/src-pipe")
			c.Check(xdelta3Args[7], Matches, "/tmp/snap-delta-.*/trgt-pipe")
			return nil
		})()

	// Execute
	err := squashfs.GenerateDelta(context.Background(), src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, IsNil)

	// Verify the delta file header was written correctly
	f, err := os.Open(deltaPath)
	c.Assert(err, IsNil)
	defer f.Close()

	header := make([]byte, 32)
	_, err = f.Read(header)
	c.Assert(err, IsNil)

	// Magic "sqpf" -> 0x66707173
	c.Check(binary.LittleEndian.Uint32(header[0:4]), Equals, uint32(0x66707173))
	// Format and tools version
	c.Check(header[4], Equals, byte(1))
	c.Check(header[5], Equals, byte(1))
	// Delta tool
	c.Check(binary.LittleEndian.Uint16(header[6:8]), Equals, uint16(1))
	// Timestamp from target snap (2000)
	c.Check(binary.LittleEndian.Uint32(header[8:12]), Equals, uint32(2000))
	// Compression
	c.Check(binary.LittleEndian.Uint16(header[12:14]), Equals, uint16(4))
	// Flags
	c.Check(binary.LittleEndian.Uint16(header[14:16]), Equals, uint16(0x0040))
}

func (s *DeltaTestSuite) TestGenerateDeltaSnapXdelta3Cancelled(c *C) {
	// Setup mock snaps
	// 4 = xz compression, 0x0040 = flagDuplicates
	src := s.createMockSnap(c, "source.snap", 1000, 4, 0x0040)
	dst := s.createMockSnap(c, "target.snap", 2000, 4, 0x0040)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "out.delta")

	// Mock the external commands
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	ctx, cancel := context.WithCancel(context.Background())

	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			// Wait for the cancellation to happen
			<-ctx.Done()
			return errors.New("calculation cancelled")
		})()

	// Cancel the context shortly after starting
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Execute
	err := squashfs.GenerateDelta(ctx, src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "calculation cancelled")
}

func (s *DeltaTestSuite) TestGenerateDeltaSnapXdelta3NoUnsquashfsCmd(c *C) {
	dst := s.createMockSnap(c, "target.snap", 2000, 4, 0x0040)

	// Mock the external commands
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return nil, errors.New("not found")
		})()
	// build should fail
	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, build func(context.Context) (
			cmds []*exec.Cmd, tasks []func() error, err error)) error {
			_, _, err := build(ctx)
			c.Assert(err, NotNil)
			return err
		})()

	// Execute
	err := squashfs.GenerateDelta(context.Background(), "source.snap", dst, "out.delta", "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "cannot find unsquashfs: not found")
}

func (s *DeltaTestSuite) TestGenerateDeltaSnapXdelta3PipeSetupError(c *C) {
	dst := s.createMockSnap(c, "target.snap", 2000, 4, 0x0040)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "out.delta")

	// Mock setupPipes
	defer squashfs.MockSetupPipes(func(pipeNames ...string) (string, []string, error) {
		return "", nil, errors.New("cannot set-up pipes")
	})()

	// Execute
	err := squashfs.GenerateDelta(context.Background(), "source.snap", dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "cannot set-up pipes")

	// Check delta file was removed
	c.Assert(osutil.FileExists(deltaPath), Equals, false)
}

func (s *DeltaTestSuite) TestGenerateDeltaTargetOpenError(c *C) {
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)
	dst := filepath.Join(dirs.GlobalRootDir, "non-existent.snap")

	err := squashfs.GenerateDelta(context.Background(), src, dst, "out.delta", "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "cannot open target: .* no such file or directory")
}

func (s *DeltaTestSuite) TestGenerateDeltaTargetReadError(c *C) {
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)
	// Create a file too small to contain the flags at offset 24
	dst := filepath.Join(dirs.GlobalRootDir, "truncated.snap")
	err := os.WriteFile(dst, []byte("too short"), 0644)
	c.Assert(err, IsNil)

	err = squashfs.GenerateDelta(context.Background(), src, dst, "out.delta", "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "while reading target superblock: unexpected EOF")
}

func (s *DeltaTestSuite) TestGenerateDeltaCreateOutputFileError(c *C) {
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)
	dst := s.createMockSnap(c, "target.snap", 2000, 1, 0)

	// Use a path that is impossible to create (directory doesn't exist)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "no-such-dir", "out.delta")

	err := squashfs.GenerateDelta(context.Background(), src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "cannot create delta file: .* no such file or directory")
}

func (s *DeltaTestSuite) TestGenerateDeltaUnsuportedSquashfsVersion(c *C) {
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)

	dst := filepath.Join(dirs.GlobalRootDir, "target.snap")
	// SquashFS superblock is at least 96 bytes. We need up to offset 26.
	data := make([]byte, 100)
	// squashfs magic number
	binary.LittleEndian.PutUint32(data[0:4], 0x73717368)
	// major @ offset 30 (2 bytes)
	binary.LittleEndian.PutUint16(data[28:30], 4)
	// minor @ offset 32 (2 bytes)
	binary.LittleEndian.PutUint16(data[30:32], 1)

	err := os.WriteFile(dst, data, 0644)
	c.Assert(err, IsNil)

	// Use a path that is impossible to create (directory doesn't exist)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "no-such-dir", "out.delta")

	err = squashfs.GenerateDelta(context.Background(), src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "unexpected squashfs version 4.1")
}

func (s *DeltaTestSuite) TestGenerateDeltaBadTargetHeader(c *C) {
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)

	dst := filepath.Join(dirs.GlobalRootDir, "target.snap")
	// SquashFS superblock is at least 96 bytes. We need up to offset 26.
	data := make([]byte, 100)
	// We do not write the magic number.
	// major @ offset 30 (2 bytes)
	binary.LittleEndian.PutUint16(data[28:30], 4)
	// minor @ offset 32 (2 bytes)
	binary.LittleEndian.PutUint16(data[30:32], 1)

	err := os.WriteFile(dst, data, 0644)
	c.Assert(err, IsNil)

	// Use a path that is impossible to create (directory doesn't exist)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "no-such-dir", "out.delta")

	err = squashfs.GenerateDelta(context.Background(), src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "target is not a squashfs")
}

func (s *DeltaTestSuite) TestApplyDeltaPlainSuccess(c *C) {
	// Write file with just xdelta3 magic
	xdelta3DiffPath := filepath.Join(dirs.GlobalRootDir, "diff.xdelta3")
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0x00c4c3d6))
	err := os.WriteFile(xdelta3DiffPath, buf.Bytes(), 0644)
	c.Assert(err, IsNil)

	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			c.Check(cmd, Equals, "/usr/bin/xdelta3")
			c.Check(args[0:4], DeepEquals, []string{"-f", "-d", "-s", "source.snap"})
			c.Check(args[4], Matches, ".*/diff.xdelta3")
			c.Check(args[5], Equals, "target.snap")
			return &exec.Cmd{
				Path: cmd,
				Args: append([]string{cmd}, args...),
			}, nil
		})()
	defer squashfs.MockCmdRun(func(cmd *exec.Cmd) error {
		return nil
	})()

	// Execute
	err = squashfs.ApplyDelta(context.Background(), "source.snap", xdelta3DiffPath, "target.snap")
	c.Assert(err, IsNil)
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3Success(c *C) {
	// Create a mock delta: gzip (1), duplicate flags set (0x0040), timestamp 5000
	deltaPath := s.createDeltaFile(c, "valid.delta", 5000, 1, 0x0040)
	sourceSnap := filepath.Join(dirs.GlobalRootDir, "source.snap")
	err := os.WriteFile(sourceSnap, []byte("mock source"), 0644)
	c.Assert(err, IsNil)

	// Mock commands
	snapdBinDir := "/usr/bin/"
	unsquashfsPath := filepath.Join(snapdBinDir, "unsquashfs")
	xdelta3Path := filepath.Join(snapdBinDir, "xdelta3")
	mksquashfsPath := filepath.Join(snapdBinDir, "mksquashfs")
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			cmds, tasks, err := buildWithContext(ctx)
			c.Assert(err, IsNil)
			c.Check(len(cmds), Equals, 3)
			c.Check(len(tasks), Equals, 1) // The deltaWriter goroutine

			// 1. Check Unsquashfs args
			c.Check(cmds[0].Path, Equals, unsquashfsPath)
			c.Check(cmds[0].Args, DeepEquals, []string{
				unsquashfsPath, "-no-progress", "-pf", cmds[0].Args[3], sourceSnap,
			})

			// 2. Check Mksquashfs args
			// Should have: -comp gzip, -mkfs-time 5000.
			// Since flags 0x0040 (duplicates) is set, -no-duplicates should NOT be present.
			c.Check(cmds[1].Path, Equals, mksquashfsPath)
			expectedMksquashArgs := []string{
				mksquashfsPath, "-", "target.snap", "-pf", "-", "-noappend", "-quiet",
				"-comp", "gzip", "-mkfs-time", "5000",
			}
			c.Check(cmds[1].Args, DeepEquals, expectedMksquashArgs)

			// 3. Check Xdelta3 args
			c.Check(cmds[2].Path, Equals, xdelta3Path)
			c.Check(len(cmds[2].Args), Equals, 6)
			c.Check(cmds[2].Args[0:3], DeepEquals, []string{xdelta3Path, "-d", "-f"})
			c.Check(cmds[2].Args[4], Matches, `/tmp/snap-delta-.*/src`)
			c.Check(cmds[2].Args[5], Matches, `/tmp/snap-delta-.*/delta`)

			// Create the target file so that growSnapToMinSize can stat it.
			return os.WriteFile("target.snap", make([]byte, squashfs.MinimumSnapSize), 0644)
		})()

	err = squashfs.ApplyDelta(context.Background(), sourceSnap, deltaPath, "target.snap")
	c.Assert(err, IsNil)
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3Cancelled(c *C) {
	// Create a mock delta: gzip (1), duplicate flags set (0x0040), timestamp 5000
	deltaPath := s.createDeltaFile(c, "valid.delta", 5000, 1, 0x0040)
	sourceSnap := filepath.Join(dirs.GlobalRootDir, "source.snap")
	err := os.WriteFile(sourceSnap, []byte("mock source"), 0644)
	c.Assert(err, IsNil)

	// Mock commands
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	ctx, cancel := context.WithCancel(context.Background())

	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			// Wait for the cancellation to happen
			<-ctx.Done()
			return errors.New("calculation cancelled")
		})()

	// Cancel the context shortly after starting
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = squashfs.ApplyDelta(ctx, sourceSnap, deltaPath, "target.snap")
	c.Assert(err, ErrorMatches, "calculation cancelled")
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3DeltaWriter(c *C) {
	// Create a mock delta file: gzip (1), timestamp 5000, and some random
	// data (instead of the xdelta3 stream) following the 32-byte header.
	deltaPath := s.createDeltaFile(c, "writer.delta", 5000, 1, 0x0040)
	expectedData := []byte("this-is-the-raw-xdelta3-payload")

	f, err := os.OpenFile(deltaPath, os.O_APPEND|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	_, err = f.Write(expectedData)
	f.Close()
	c.Assert(err, IsNil)

	sourceSnap := filepath.Join(dirs.GlobalRootDir, "source.snap")
	err = os.WriteFile(sourceSnap, []byte("source"), 0644)
	c.Assert(err, IsNil)

	// Mock the command creation to avoid needing real binaries.
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	// Mock RunManyWithContext to manually trigger the deltaWriter task.
	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			cmds, tasks, err := buildWithContext(ctx)
			c.Assert(err, IsNil)
			// Ensure deltaWriter task exists (it should be the only task in applyXdelta3Delta).
			c.Assert(tasks, HasLen, 1)

			// We need to simulate the consumer (xdelta3) reading from the pipe
			// so the deltaWriter doesn't block forever on the FIFO write.
			// The deltaPipe path is found in the arguments of the xdelta3 command (cmds[2]).
			c.Assert(len(cmds), Equals, 3)
			c.Assert(cmds[2].Path, Equals, "/usr/bin/xdelta3")
			c.Assert(len(cmds[2].Args), Equals, 6)
			c.Assert(cmds[2].Args[0:3], DeepEquals, []string{"/usr/bin/xdelta3", "-d", "-f"})
			deltaPipePath := cmds[2].Args[5]

			// Start a reader in the background to capture what the deltaWriter sends.
			readResult := make(chan []byte)
			go func() {
				r, _ := os.Open(deltaPipePath)
				defer r.Close()
				data, _ := io.ReadAll(r)
				readResult <- data
			}()

			// Run the deltaWriter routine provided by applyXdelta3Delta.
			err = tasks[0]()
			c.Assert(err, IsNil)

			// Validate that the data read from the pipe matches the payload
			// (skipping the 32-byte header).
			pipeData := <-readResult
			c.Check(string(pipeData), Equals, string(expectedData))

			// Create the target file so that growSnapToMinSize can stat it.
			return os.WriteFile("target.snap", make([]byte, squashfs.MinimumSnapSize), 0644)
		})()

	// Execute
	err = squashfs.ApplyDelta(context.Background(), sourceSnap, deltaPath, "target.snap")
	c.Assert(err, IsNil)
}

func (s *DeltaTestSuite) TestApplyDeltaShortFile(c *C) {
	deltaPath := filepath.Join(dirs.GlobalRootDir, "short.delta")
	err := os.WriteFile(deltaPath, []byte{0x01, 0x02}, 0644)
	c.Assert(err, IsNil)

	err = squashfs.ApplyDelta(context.Background(), "source.snap", deltaPath, "target.snap")
	c.Assert(err, ErrorMatches, "delta file does not contain a header")
}

func (s *DeltaTestSuite) TestApplyDeltaUnknownMagic(c *C) {
	deltaPath := filepath.Join(dirs.GlobalRootDir, "unknown.delta")
	// Random 4 bytes that aren't xdelta3 or sqpf magic
	err := os.WriteFile(deltaPath, []byte{0xDE, 0xAD, 0xBE, 0xEF}, 0644)
	c.Assert(err, IsNil)

	err = squashfs.ApplyDelta(context.Background(), "source.snap", deltaPath, "target.snap")
	c.Assert(err, ErrorMatches, "unknown delta file format")
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3VersionMismatch(c *C) {
	path := filepath.Join(dirs.GlobalRootDir, "bad-version.delta")
	buf := new(bytes.Buffer)
	// Magic
	binary.Write(buf, binary.LittleEndian, uint32(0x66707173))
	// Version 2.1 (Unsupported)
	binary.Write(buf, binary.LittleEndian, uint8(0x02))
	binary.Write(buf, binary.LittleEndian, uint8(0x00))
	// Delta Tool (u16) - 0x01 for Xdelta3
	binary.Write(buf, binary.LittleEndian, uint16(0x01))
	// Timestamp (u32)
	binary.Write(buf, binary.LittleEndian, uint32(1000))
	// Compression (u16)
	binary.Write(buf, binary.LittleEndian, uint16(1))
	// Flags (u16)
	binary.Write(buf, binary.LittleEndian, uint16(0))
	// Padding to header size
	buf.Write(make([]byte, 16))
	err := os.WriteFile(path, buf.Bytes(), 0644)
	c.Assert(err, IsNil)

	err = squashfs.ApplyDelta(context.Background(), "source.snap", path, "target.snap")
	c.Assert(err, ErrorMatches, `incompatible version 2.0`)
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3UnsupportedTool(c *C) {
	// Create delta with Tool ID 99
	deltaPath := s.createDeltaFile(c, "bad-tool.delta", 1234, 1, 0x0040)
	// Manually overwrite the tool ID in the file (offset 6, 2 bytes)
	f, err := os.OpenFile(deltaPath, os.O_RDWR, 0644)
	c.Assert(err, IsNil)
	_, err = f.WriteAt([]byte{0x63, 0x00}, 6) // 0x0063 = 99
	f.Close()
	c.Assert(err, IsNil)

	err = squashfs.ApplyDelta(context.Background(), "source.snap", deltaPath, "target.snap")
	c.Assert(err, ErrorMatches, "unsupported delta tool 99")
}

func (s *DeltaTestSuite) TestApplyDeltaInvalidCompressionInHeader(c *C) {
	// Compression ID 99 is unknown
	deltaPath := s.createDeltaFile(c, "bad-comp.delta", 1234, 99, 0x0040)

	err := squashfs.ApplyDelta(context.Background(), "source.snap", deltaPath, "target.snap")
	c.Assert(err, ErrorMatches,
		"bad compression id from delta header: unknown compression id: 99")
}

func (s *DeltaTestSuite) TestApplyDeltaInvalidFlagsInHeader(c *C) {
	// flagCheck (0x0004) triggers an error in superBlockFlagsToMksquashfsArgs
	deltaPath := s.createDeltaFile(c, "bad-flags.delta", 1234, 1, 0x0004)

	err := squashfs.ApplyDelta(context.Background(), "source.snap", deltaPath, "target.snap")
	c.Assert(err, ErrorMatches,
		"bad flags from delta header: unexpected value in superblock flags")
}

func (s *DeltaTestSuite) TestGenerateDeltaTargetUnsupportedCompressionOptions(c *C) {
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)
	// Create a target snap with the flagCompressorOptions bit set (0x0400)
	dst := s.createMockSnap(c, "target_with_opts.snap", 2000, 1, 0x0400)

	deltaPath := filepath.Join(dirs.GlobalRootDir, "out.delta")

	err := squashfs.GenerateDelta(context.Background(), src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "compression options section present in target, which is unsupported")
}

func (s *DeltaTestSuite) TestGenerateDeltaSnapXdelta3RunManyError(c *C) {
	// Setup mock snaps and paths
	src := s.createMockSnap(c, "source.snap", 1000, 1, 0)
	dst := s.createMockSnap(c, "target.snap", 2000, 1, 0)
	deltaPath := filepath.Join(dirs.GlobalRootDir, "out.delta")

	// Capture the temp directory created during the process
	var capturedTempDir string
	defer squashfs.MockSetupPipes(func(pipeNames ...string) (string, []string, error) {
		tempDir, pipes, err := squashfs.SetupPipes(pipeNames...)
		capturedTempDir = tempDir
		return tempDir, pipes, err
	})()

	// Mock the external commands to return valid exec.Cmd objects
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	// Force RunManyWithContext to fail
	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, build func(context.Context) (
			cmds []*exec.Cmd, tasks []func() error, err error)) error {
			build(ctx)
			return errors.New("pipeline execution failed")
		})()

	// Execute
	err := squashfs.GenerateDelta(context.Background(), src, dst, deltaPath, "snap-1-1-xdelta3")
	c.Assert(err, ErrorMatches, "pipeline execution failed")

	// The captured temp directory should no longer exist
	c.Assert(capturedTempDir, Not(Equals), "")
	_, err = os.Stat(capturedTempDir)
	c.Check(os.IsNotExist(err), Equals, true,
		Commentf("Temp dir %s was not cleaned up", capturedTempDir))
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3PadsToMinSize(c *C) {
	deltaPath := s.createDeltaFile(c, "valid.delta", 5000, 1, 0x0040)
	sourceSnap := filepath.Join(dirs.GlobalRootDir, "source.snap")
	err := os.WriteFile(sourceSnap, []byte("mock source"), 0644)
	c.Assert(err, IsNil)

	targetSnap := filepath.Join(dirs.GlobalRootDir, "target.snap")

	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			_, _, err := buildWithContext(ctx)
			c.Assert(err, IsNil)
			// Simulate mksquashfs creating a small squashfs (smaller
			// than MinimumSnapSize).
			return os.WriteFile(targetSnap, make([]byte, 4096), 0644)
		})()

	err = squashfs.ApplyDelta(context.Background(), sourceSnap, deltaPath, targetSnap)
	c.Assert(err, IsNil)

	fi, err := os.Stat(targetSnap)
	c.Assert(err, IsNil)
	c.Check(fi.Size(), Equals, squashfs.MinimumSnapSize)
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3NoPadIfLargeEnough(c *C) {
	deltaPath := s.createDeltaFile(c, "valid.delta", 5000, 1, 0x0040)
	sourceSnap := filepath.Join(dirs.GlobalRootDir, "source.snap")
	err := os.WriteFile(sourceSnap, []byte("mock source"), 0644)
	c.Assert(err, IsNil)

	targetSnap := filepath.Join(dirs.GlobalRootDir, "target.snap")
	largeSize := int64(squashfs.MinimumSnapSize * 2)

	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, buildWithContext func(context.Context) ([]*exec.Cmd, []func() error, error)) error {
			_, _, err := buildWithContext(ctx)
			c.Assert(err, IsNil)
			// Simulate mksquashfs creating a snap already larger than MinimumSnapSize.
			return os.WriteFile(targetSnap, make([]byte, largeSize), 0644)
		})()

	err = squashfs.ApplyDelta(context.Background(), sourceSnap, deltaPath, targetSnap)
	c.Assert(err, IsNil)

	fi, err := os.Stat(targetSnap)
	c.Assert(err, IsNil)
	c.Check(fi.Size(), Equals, largeSize)
}

func (s *DeltaTestSuite) TestApplyDeltaSnapXdelta3RunManyError(c *C) {
	// Setup mock snaps and delta
	deltaPath := s.createDeltaFile(c, "error.delta", 5000, 1, 0x0040)
	sourceSnap := filepath.Join(dirs.GlobalRootDir, "source.snap")
	err := os.WriteFile(sourceSnap, []byte("mock source"), 0644)
	c.Assert(err, IsNil)

	// Capture the temp directory created during the process
	var capturedTempDir string
	defer squashfs.MockSetupPipes(func(pipeNames ...string) (string, []string, error) {
		tempDir, pipes, err := squashfs.SetupPipes(pipeNames...)
		capturedTempDir = tempDir
		return tempDir, pipes, err
	})()

	// Mock the external commands to return valid exec.Cmd objects
	defer squashfs.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, cmd string, args ...string) (*exec.Cmd, error) {
			return &exec.Cmd{Path: cmd, Args: append([]string{cmd}, args...)}, nil
		})()

	// Force RunManyWithContext to fail
	defer squashfs.MockOsutilRunManyWithContext(
		func(ctx context.Context, build func(context.Context) (
			cmds []*exec.Cmd, tasks []func() error, err error)) error {
			build(ctx)
			return errors.New("apply pipeline failed")
		})()

	// Execute
	err = squashfs.ApplyDelta(context.Background(), sourceSnap, deltaPath, "target.snap")
	c.Assert(err, ErrorMatches, "apply pipeline failed")

	// The captured temp directory should no longer exist
	c.Assert(capturedTempDir, Not(Equals), "")
	_, err = os.Stat(capturedTempDir)
	c.Check(os.IsNotExist(err), Equals, true,
		Commentf("Temp dir %s was not cleaned up during ApplyDelta failure", capturedTempDir))
}
