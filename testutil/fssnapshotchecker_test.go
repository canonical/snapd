// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package testutil_test

import (
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type fsSnapshotCheckerSuite struct{}

var _ = check.Suite(&fsSnapshotCheckerSuite{})

func (*fsSnapshotCheckerSuite) TestFsSnapshotsEqualInfo(c *check.C) {
	testInfo(c, FsSnapshotsEqual, "FsSnapshotsEqual", []string{"after", "before", "ignoreDiff"})
}

func (*fsSnapshotCheckerSuite) TestFsSnapshotsEqualBadTypes(c *check.C) {
	snapshot := make(FsSnapshot)
	testCheck(c, FsSnapshotsEqual, false, "after value must be of type testutil.FsSnapshot", 42, snapshot, nil)
	testCheck(c, FsSnapshotsEqual, false, "before value must be of type testutil.FsSnapshot", snapshot, 42, nil)
	testCheck(c, FsSnapshotsEqual, false, "ignoreDiff value must be of type *testutil.FsSnapshotIgnoreDiff or nil", snapshot, snapshot, 42)
}

func (*fsSnapshotCheckerSuite) TestCreateFsSnapshot(c *check.C) {
	d := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(d, "dir"), 0755), check.IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "file1"), []byte("hello"), 0644), check.IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "dir", "file2"), []byte("world"), 0600), check.IsNil)

	snapshot, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	// Root ".", subdir "dir", and two files
	c.Check(snapshot, check.HasLen, 4)
}

func (*fsSnapshotCheckerSuite) TestIdenticalFsSnapshotsMatch(c *check.C) {
	d := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(d, "file"), []byte("data"), 0644), check.IsNil)

	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	testCheck(c, FsSnapshotsEqual, true, "", after, before, nil)
}

func (*fsSnapshotCheckerSuite) TestNewFile(c *check.C) {
	d := c.MkDir()
	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.WriteFile(filepath.Join(d, "file"), []byte("x"), 0644), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	testCheck(c, FsSnapshotsEqual, false, "filesystem snapshots differ:\n  <path>: <difference kinds>\n  file: presence (added)\n", after, before, nil)
}

func (*fsSnapshotCheckerSuite) TestRemovedFile(c *check.C) {
	d := c.MkDir()
	f := filepath.Join(d, "file")
	c.Assert(os.WriteFile(f, []byte("x"), 0644), check.IsNil)

	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.Remove(f), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	testCheck(c, FsSnapshotsEqual, false, "filesystem snapshots differ:\n  <path>: <difference kinds>\n  file: presence (removed)\n", after, before, nil)
}

func (*fsSnapshotCheckerSuite) TestChangedContent(c *check.C) {
	d := c.MkDir()
	f := filepath.Join(d, "file")
	c.Assert(os.WriteFile(f, []byte("aaa"), 0644), check.IsNil)

	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.WriteFile(f, []byte("bbb"), 0644), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	testCheck(c, FsSnapshotsEqual, false, "filesystem snapshots differ:\n  <path>: <difference kinds>\n  file: content (\"aaa\" -> \"bbb\")\n", after, before, nil)
}

func (*fsSnapshotCheckerSuite) TestChangedSize(c *check.C) {
	d := c.MkDir()
	f := filepath.Join(d, "file")
	c.Assert(os.WriteFile(f, []byte("short"), 0644), check.IsNil)

	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.WriteFile(f, []byte("much longer content"), 0644), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	testCheck(c, FsSnapshotsEqual, false, "filesystem snapshots differ:\n  <path>: <difference kinds>\n  file: size (5 -> 19), content (\"short\" -> \"much longer content\")\n", after, before, nil)
}

func (*fsSnapshotCheckerSuite) TestChangedMode(c *check.C) {
	d := c.MkDir()
	f := filepath.Join(d, "file")
	c.Assert(os.WriteFile(f, []byte("x"), 0644), check.IsNil)

	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.Chmod(f, 0600), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	testCheck(c, FsSnapshotsEqual, false, "filesystem snapshots differ:\n  <path>: <difference kinds>\n  file: mode (0644 -> 0600)\n", after, before, nil)
}

func (*fsSnapshotCheckerSuite) TestIgnorePresence(c *check.C) {
	d := c.MkDir()
	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.WriteFile(filepath.Join(d, "file"), []byte("x"), 0644), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	ignore := &FsSnapshotIgnoreDiff{
		"file": {Kinds: []FsDiffKind{PresenceDiffKind}},
	}
	testCheck(c, FsSnapshotsEqual, true, "", after, before, ignore)
}

func (*fsSnapshotCheckerSuite) TestIgnoreParents(c *check.C) {
	d := c.MkDir()
	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.MkdirAll(filepath.Join(d, "a", "b"), 0755), check.IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "a", "b", "file"), []byte("x"), 0644), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	// Ignoring "a/b/file" with IgnoreParents should also ignore "a/b" and "a"
	ignore := &FsSnapshotIgnoreDiff{
		"a/b/file": {Kinds: []FsDiffKind{PresenceDiffKind}, IgnoreParents: true},
	}
	testCheck(c, FsSnapshotsEqual, true, "", after, before, ignore)
}

func (*fsSnapshotCheckerSuite) TestLargeFileContentDiff(c *check.C) {
	d := c.MkDir()
	f := filepath.Join(d, "file")

	// Create files larger than maxContentPreviewSize (256 bytes)
	bigA := make([]byte, 300)
	bigB := make([]byte, 300)
	for i := range bigA {
		bigA[i] = 'a'
		bigB[i] = 'b'
	}
	c.Assert(os.WriteFile(f, bigA, 0644), check.IsNil)

	before, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	c.Assert(os.WriteFile(f, bigB, 0644), check.IsNil)
	after, err := CreateFsSnapshot(d)
	c.Assert(err, check.IsNil)

	// Should show just "content" without preview for large files
	result, errStr := FsSnapshotsEqual.Check([]any{after, before, nil}, []string{"after", "before", "ignoreDiff"})
	c.Check(result, check.Equals, false)
	c.Check(errStr, check.Matches, "(?s).*file: content\n.*")
}
