// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package osutil_test

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type EnsureDirStateSuite struct {
	dir  string
	glob string
}

var _ = Suite(&EnsureDirStateSuite{glob: "*.snap"})

func (s *EnsureDirStateSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
}

func (s *EnsureDirStateSuite) TestVerifiesExpectedFiles(c *C) {
	name := filepath.Join(s.dir, "expected.snap")
	mylog.Check(os.WriteFile(name, []byte("expected"), 0600))

	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"expected.snap": &osutil.MemoryFileState{Content: []byte("expected"), Mode: 0600},
	}))

	// Report says that nothing has changed
	c.Assert(changed, HasLen, 0)
	c.Assert(removed, HasLen, 0)
	// The content is correct
	c.Assert(path.Join(s.dir, "expected.snap"), testutil.FileEquals, "expected")
	// The permissions are correct
	stat := mylog.Check2(os.Stat(name))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureDirStateSuite) TestTwoPatterns(c *C) {
	name1 := filepath.Join(s.dir, "expected.snap")
	mylog.Check(os.WriteFile(name1, []byte("expected-1"), 0600))


	name2 := filepath.Join(s.dir, "expected.snap-update-ns")
	mylog.Check(os.WriteFile(name2, []byte("expected-2"), 0600))


	changed, removed := mylog.Check3(osutil.EnsureDirStateGlobs(s.dir, []string{"*.snap", "*.snap-update-ns"}, map[string]osutil.FileState{
		"expected.snap":           &osutil.MemoryFileState{Content: []byte("expected-1"), Mode: 0600},
		"expected.snap-update-ns": &osutil.MemoryFileState{Content: []byte("expected-2"), Mode: 0600},
	}))

	// Report says that nothing has changed
	c.Assert(changed, HasLen, 0)
	c.Assert(removed, HasLen, 0)
	// The content is correct
	c.Assert(name1, testutil.FileEquals, "expected-1")
	c.Assert(name2, testutil.FileEquals, "expected-2")
	// The permissions are correct
	stat := mylog.Check2(os.Stat(name1))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
	stat = mylog.Check2(os.Stat(name2))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureDirStateSuite) TestMultipleMatches(c *C) {
	name := filepath.Join(s.dir, "foo")
	mylog.Check(os.WriteFile(name, []byte("content"), 0600))

	// When a file is matched by multiple globs it removed correctly.
	changed, removed := mylog.Check3(osutil.EnsureDirStateGlobs(s.dir, []string{"foo", "f*"}, nil))

	c.Assert(changed, HasLen, 0)
	c.Assert(removed, DeepEquals, []string{"foo"})
}

func (s *EnsureDirStateSuite) TestCreatesMissingFiles(c *C) {
	name := filepath.Join(s.dir, "missing.snap")
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"missing.snap": &osutil.MemoryFileState{Content: []byte(`content`), Mode: 0600},
	}))

	// Created file is reported
	c.Assert(changed, DeepEquals, []string{"missing.snap"})
	c.Assert(removed, HasLen, 0)
	// The content is correct
	c.Assert(name, testutil.FileEquals, "content")
	// The permissions are correct
	stat := mylog.Check2(os.Stat(name))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureDirStateSuite) TestRemovesUnexpectedFiless(c *C) {
	name := filepath.Join(s.dir, "evil.snap")
	mylog.Check(os.WriteFile(name, []byte(`evil text`), 0600))

	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{}))

	// Removed file is reported
	c.Assert(changed, HasLen, 0)
	c.Assert(removed, DeepEquals, []string{"evil.snap"})
	// The file is removed
	_ = mylog.Check2(os.Stat(name))
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *EnsureDirStateSuite) TestIgnoresUnrelatedFiles(c *C) {
	name := filepath.Join(s.dir, "unrelated")
	mylog.Check(os.WriteFile(name, []byte(`text`), 0600))

	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{}))

	// Report says that nothing has changed
	c.Assert(changed, HasLen, 0)
	c.Assert(removed, HasLen, 0)
	// The file is still there
	_ = mylog.Check2(os.Stat(name))

}

func (s *EnsureDirStateSuite) TestCorrectsFilesWithDifferentSize(c *C) {
	name := filepath.Join(s.dir, "differing.snap")
	mylog.Check(os.WriteFile(name, []byte(``), 0600))

	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"differing.snap": &osutil.MemoryFileState{Content: []byte(`Hello World`), Mode: 0600},
	}))

	// changed file is reported
	c.Assert(changed, DeepEquals, []string{"differing.snap"})
	c.Assert(removed, HasLen, 0)
	// The content is changed
	c.Assert(name, testutil.FileEquals, "Hello World")
	// The permissions are what we expect
	stat := mylog.Check2(os.Stat(name))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureDirStateSuite) TestCorrectsFilesWithSameSize(c *C) {
	name := filepath.Join(s.dir, "differing.snap")
	mylog.Check(os.WriteFile(name, []byte("evil"), 0600))

	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"differing.snap": &osutil.MemoryFileState{Content: []byte("good"), Mode: 0600},
	}))

	// changed file is reported
	c.Assert(changed, DeepEquals, []string{"differing.snap"})
	c.Assert(removed, HasLen, 0)
	// The content is changed
	c.Assert(name, testutil.FileEquals, "good")
	// The permissions are what we expect
	stat := mylog.Check2(os.Stat(name))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureDirStateSuite) TestFixesFilesWithBadPermissions(c *C) {
	name := filepath.Join(s.dir, "sensitive.snap")
	mylog.
		// NOTE: the existing file is currently wide-open for everyone"
		Check(os.WriteFile(name, []byte("password"), 0666))

	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		// NOTE: we want the file to be private
		"sensitive.snap": &osutil.MemoryFileState{Content: []byte("password"), Mode: 0600},
	}))

	// changed file is reported
	c.Assert(changed, DeepEquals, []string{"sensitive.snap"})
	c.Assert(removed, HasLen, 0)
	// The content is still the same
	c.Assert(name, testutil.FileEquals, "password")
	// The permissions are changed
	stat := mylog.Check2(os.Stat(name))

	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureDirStateSuite) TestReportsAbnormalFileLocation(c *C) {
	_, _ := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{"subdir/file.snap": &osutil.MemoryFileState{}}))
	c.Assert(err, ErrorMatches, `internal error: EnsureDirState got filename "subdir/file.snap" which has a path component`)
}

func (s *EnsureDirStateSuite) TestReportsAbnormalFileName(c *C) {
	_, _ := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{"without-namespace": &osutil.MemoryFileState{}}))
	c.Assert(err, ErrorMatches, `internal error: EnsureDirState got filename "without-namespace" which doesn't match the glob pattern "\*\.snap"`)
}

func (s *EnsureDirStateSuite) TestReportsAbnormalPatterns(c *C) {
	_, _ := mylog.Check3(osutil.EnsureDirState(s.dir, "[", nil))
	c.Assert(err, ErrorMatches, `internal error: EnsureDirState got invalid pattern "\[": syntax error in pattern`)
}

func (s *EnsureDirStateSuite) TestRemovesAllManagedFilesOnError(c *C) {
	// Create a "prior.snap" file
	prior := filepath.Join(s.dir, "prior.snap")
	mylog.Check(os.WriteFile(prior, []byte("data"), 0600))

	// Create a "clash.snap" directory to simulate failure
	clash := filepath.Join(s.dir, "clash.snap")
	mylog.Check(os.Mkdir(clash, 0000))

	// Try to ensure directory state
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"prior.snap": &osutil.MemoryFileState{Content: []byte("data"), Mode: 0600},
		"clash.snap": &osutil.MemoryFileState{Content: []byte("data"), Mode: 0600},
	}))
	c.Assert(changed, HasLen, 0)
	c.Assert(removed, DeepEquals, []string{"clash.snap", "prior.snap"})
	c.Assert(err, ErrorMatches, "open .*/clash.snap: permission denied")
	// The clashing file is removed
	_ = mylog.Check2(os.Stat(clash))
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *EnsureDirStateSuite) TestRemovesSymlink(c *C) {
	original := filepath.Join(s.dir, "original.snap")
	mylog.Check(os.WriteFile(original, []byte("data"), 0600))


	symlink := filepath.Join(s.dir, "symlink.snap")
	mylog.Check(os.Symlink(original, symlink))


	// Removed file is reported
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"original.snap": &osutil.FileReference{Path: original},
	}))

	c.Check(len(changed), Equals, 0)
	c.Check(len(removed), Equals, 1)
	c.Check(removed[0], Equals, "symlink.snap")

	c.Check(symlink, testutil.FileAbsent)
	c.Check(original, testutil.FileEquals, "data")
}

func (s *EnsureDirStateSuite) TestCreatesMissingSymlink(c *C) {
	original := filepath.Join(s.dir, "original.snap")
	mylog.Check(os.WriteFile(original, []byte("data"), 0600))


	// Created file is reported
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"original.snap":        &osutil.FileReference{Path: original},
		"missing-symlink.snap": &osutil.SymlinkFileState{Target: original},
	}))

	c.Assert(changed, DeepEquals, []string{"missing-symlink.snap"})
	c.Assert(removed, HasLen, 0)

	// The symlink is created
	missingSymlink := filepath.Join(s.dir, "missing-symlink.snap")
	c.Assert(missingSymlink, testutil.FileEquals, "data")
	c.Assert(osutil.IsSymlink(missingSymlink), Equals, true)
	// and points to original
	link := mylog.Check2(os.Readlink(missingSymlink))

	c.Assert(link, Equals, original)
}

func (s *EnsureDirStateSuite) TestReplaceFileWithSymlink(c *C) {
	original := filepath.Join(s.dir, "original.snap")
	mylog.Check(os.WriteFile(original, []byte("data"), 0600))


	symlink := filepath.Join(s.dir, "symlink.snap")
	mylog.Check(os.WriteFile(symlink, []byte("old-data"), 0600))


	// Changed file is reported
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"original.snap": &osutil.FileReference{Path: original},
		"symlink.snap":  &osutil.SymlinkFileState{Target: original},
	}))

	c.Assert(changed, DeepEquals, []string{"symlink.snap"})
	c.Assert(removed, HasLen, 0)

	// The symlink is created
	c.Assert(symlink, testutil.FileEquals, "data")
	c.Assert(osutil.IsSymlink(symlink), Equals, true)
	// and points to original
	link := mylog.Check2(os.Readlink(symlink))

	c.Assert(link, Equals, original)
}

func (s *EnsureDirStateSuite) TestReplaceSymlinkWithSymlink(c *C) {
	symlink := filepath.Join(s.dir, "symlink.snap")
	mylog.Check(os.Symlink("old-target", symlink))


	// Changed file is reported
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"symlink.snap": &osutil.SymlinkFileState{Target: "new-target"},
	}))

	c.Assert(changed, DeepEquals, []string{"symlink.snap"})
	c.Assert(removed, HasLen, 0)

	// The symlink points to new target
	link := mylog.Check2(os.Readlink(symlink))

	c.Assert(link, Equals, "new-target")
}

func (s *EnsureDirStateSuite) TestSameSymlink(c *C) {
	symlink := filepath.Join(s.dir, "symlink.snap")
	mylog.Check(os.Symlink("target", symlink))


	// Changed file is reported
	changed, removed := mylog.Check3(osutil.EnsureDirState(s.dir, s.glob, map[string]osutil.FileState{
		"symlink.snap": &osutil.SymlinkFileState{Target: "target"},
	}))

	c.Assert(changed, HasLen, 0)
	c.Assert(removed, HasLen, 0)

	// The symlink doesn't change
	link := mylog.Check2(os.Readlink(symlink))

	c.Assert(link, Equals, "target")
}

type mockFileState struct {
	reader io.ReadCloser
	size   int64
	mode   os.FileMode
	err    error
}

func (mock *mockFileState) State() (io.ReadCloser, int64, os.FileMode, error) {
	return mock.reader, mock.size, mock.mode, mock.err
}

func (s *EnsureDirStateSuite) TestUnsupportedFileMode(c *C) {
	unsupportedModeTypes := []os.FileMode{
		os.ModeDir,
		os.ModeNamedPipe,
		os.ModeSocket,
		os.ModeDevice,
		os.ModeCharDevice,
		os.ModeIrregular,
	}
	filePath := filepath.Join(s.dir, "test.snap")
	for _, modeType := range unsupportedModeTypes {
		fileState := &mockFileState{mode: modeType}
		mylog.Check(osutil.EnsureFileState(filePath, fileState))
		expectedErr := fmt.Sprintf("internal error: EnsureFileState does not support type %q", modeType)
		c.Check(err.Error(), Equals, expectedErr)
	}
}

func (s *EnsureDirStateSuite) TestFileReferenceUnsupportedFileMode(c *C) {
	// Directories are unsupported
	testPath := filepath.Join(s.dir, "test.dir")
	c.Assert(os.MkdirAll(testPath, 0755), IsNil)
	fref := osutil.FileReference{Path: testPath}
	_, _, _ := mylog.Check4(fref.State())
	c.Check(err, ErrorMatches, fmt.Sprintf("internal error: only regular files are supported, got %q instead", os.ModeDir))

	// Pipes are unsupported
	testPath = filepath.Join(s.dir, "test.pipe")
	c.Assert(syscall.Mkfifo(testPath, 0600), IsNil)
	// We need to open a writer to avoid getting stuck opening file
	file := mylog.Check2(os.OpenFile(testPath, os.O_RDWR, 0))

	defer file.Close()
	fref = osutil.FileReference{Path: testPath}
	_, _, _ = mylog.Check4(fref.State())
	c.Check(err, ErrorMatches, fmt.Sprintf("internal error: only regular files are supported, got %q instead", os.ModeNamedPipe))
}

func (s *EnsureDirStateSuite) TestFileReferencePlusModeUnsupportedFileMode(c *C) {
	testPath := filepath.Join(s.dir, "test.dir")
	c.Assert(os.WriteFile(testPath, []byte("test"), 0600), IsNil)

	unsupportedModeTypes := []os.FileMode{
		os.ModeDir,
		os.ModeNamedPipe,
		os.ModeSocket,
		os.ModeDevice,
		os.ModeCharDevice,
		os.ModeIrregular,
	}

	for _, modeType := range unsupportedModeTypes {
		fref := osutil.FileReferencePlusMode{
			FileReference: osutil.FileReference{Path: testPath},
			Mode:          modeType,
		}
		_, _, _ := mylog.Check4(fref.State())
		c.Check(err.Error(), Equals, fmt.Sprintf("internal error: only regular files are supported, got %q instead", modeType))
	}
}

func (s *EnsureDirStateSuite) TestMemoryFileStateUnsupportedFileMode(c *C) {
	testPath := filepath.Join(s.dir, "test.dir")
	c.Assert(os.WriteFile(testPath, []byte("test"), 0600), IsNil)

	unsupportedModeTypes := []os.FileMode{
		os.ModeDir,
		os.ModeNamedPipe,
		os.ModeSocket,
		os.ModeDevice,
		os.ModeCharDevice,
		os.ModeIrregular,
	}

	for _, modeType := range unsupportedModeTypes {
		blob := osutil.MemoryFileState{
			Mode: modeType,
		}
		_, _, _ := mylog.Check4(blob.State())
		c.Check(err.Error(), Equals, fmt.Sprintf("internal error: only regular files are supported, got %q instead", modeType))
	}
}
