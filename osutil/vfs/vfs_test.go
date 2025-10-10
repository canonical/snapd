// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package vfs_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/snapcore/snapd/osutil/vfs"
)

func assertVFS(t *testing.T, v *vfs.VFS, expected string) {
	t.Helper()
	if v.String() != expected {
		t.Fatal("Unexpected VFS state", v)
	}
}

// MetaDataFS allows injecting major:minor pair or source into into any fs.StatFS.

// This allows making VFSes more readable by differentiating each file system.
type MetaDataFS struct {
	fs.StatFS
	source       string
	major, minor int
}

func (fs MetaDataFS) MajorMinor() (int, int) { return fs.major, fs.minor }
func (fs MetaDataFS) Source() string         { return fs.source }

func WithMajorMinor(major, minor int, sfs fs.StatFS) MetaDataFS {
	if sfs, ok := sfs.(MetaDataFS); ok {
		sfs.major = major
		sfs.minor = minor
		return sfs
	}

	return MetaDataFS{StatFS: sfs, major: major, minor: minor}
}

func WithSource(s string, sfs fs.StatFS) MetaDataFS {
	if sfs, ok := sfs.(MetaDataFS); ok {
		sfs.source = s
		return sfs
	}

	return MetaDataFS{StatFS: sfs, source: s}
}

func TestRbindOrder(t *testing.T) {
	// This test replicates the logic of osutil/vfs/tests/rbind-order

	// VFS has a rootfs with two directories, a and b.
	v := vfs.NewVFS(fstest.MapFS{
		"a": &fstest.MapFile{Mode: fs.ModeDir},
		"b": &fstest.MapFile{Mode: fs.ModeDir},
	})

	// a is a filesystem with three directories: 1, 2 and 3.
	afs := fstest.MapFS{
		"1": &fstest.MapFile{Mode: fs.ModeDir},
		"2": &fstest.MapFile{Mode: fs.ModeDir},
		"3": &fstest.MapFile{Mode: fs.ModeDir},
	}
	if err := v.Mount(WithMajorMinor(42, 0, afs), "a"); err != nil {
		t.Fatal(err)
	}

	// a1 is a filesystem with only one directory: 1.
	a1fs := fstest.MapFS{
		"1": &fstest.MapFile{Mode: fs.ModeDir},
	}
	if err := v.Mount(WithMajorMinor(42, 1, a1fs), "a/1"); err != nil {
		t.Fatal(err)
	}

	// a2 is a filesystem with only one directory: 2.
	a2fs := fstest.MapFS{
		"2": &fstest.MapFile{Mode: fs.ModeDir},
	}
	if err := v.Mount(WithMajorMinor(42, 2, a2fs), "a/2"); err != nil {
		t.Fatal(err)
	}

	// a11 is a filesystem with only one directory: 1.
	a11fs := fstest.MapFS{
		"1": &fstest.MapFile{Mode: fs.ModeDir},
	}
	if err := v.Mount(WithMajorMinor(42, 3, a11fs), "a/1/1"); err != nil {
		t.Fatal(err)
	}

	// a3 is an empty file system.
	a3fs := fstest.MapFS{}
	if err := v.Mount(WithMajorMinor(42, 4, a3fs), "a/3"); err != nil {
		t.Fatal(err)
	}

	// a22 is a filesystem with only one directory: 2.
	a22fs := fstest.MapFS{
		"2": &fstest.MapFile{Mode: fs.ModeDir},
	}
	if err := v.Mount(WithMajorMinor(42, 5, a22fs), "a/2/2"); err != nil {
		t.Fatal(err)
	}

	// Recursively bind mount a to b.
	if err := v.RecursiveBindMount("a", "b"); err != nil {
		t.Fatal(err)
	}

	const expected = `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 42:0 / /a rw - (fstype) (source) rw
1  0 42:1 / /a/1 rw - (fstype) (source) rw
2  0 42:2 / /a/2 rw - (fstype) (source) rw
3  1 42:3 / /a/1/1 rw - (fstype) (source) rw
4  0 42:4 / /a/3 rw - (fstype) (source) rw
5  2 42:5 / /a/2/2 rw - (fstype) (source) rw
6  -1 42:0 / /b rw - (fstype) (source) rw
7  6 42:1 / /b/1 rw - (fstype) (source) rw
8  7 42:3 / /b/1/1 rw - (fstype) (source) rw
9  6 42:2 / /b/2 rw - (fstype) (source) rw
10 9 42:5 / /b/2/2 rw - (fstype) (source) rw
11 6 42:4 / /b/3 rw - (fstype) (source) rw
`
	if v.String() != expected {
		t.Log(v)
		t.Fatal("Unexpected mount table")
	}
}

func TestBindStack(t *testing.T) {
	// This test replicates the logic of osutil/vfs/tests/bind-stack with
	// both the bind and rbind variants.

	makeVFS := func(t *testing.T) *vfs.VFS {
		t.Helper()

		// VFS has a rootfs with two directories, a and b.
		v := vfs.NewVFS(fstest.MapFS{
			"a": &fstest.MapFile{Mode: fs.ModeDir},
			"b": &fstest.MapFile{Mode: fs.ModeDir},
		})

		// a has three identical empty file-systems mounted on it.
		aXfs := WithSource("tmpfs-x", fstest.MapFS{})
		if err := v.Mount(WithMajorMinor(42, 0, aXfs), "a"); err != nil {
			t.Fatal(err)
		}

		aYfs := WithSource("tmpfs-y", fstest.MapFS{})
		if err := v.Mount(WithMajorMinor(42, 1, aYfs), "a"); err != nil {
			t.Fatal(err)
		}

		aZfs := WithSource("tmpfs-z", fstest.MapFS{})
		if err := v.Mount(WithMajorMinor(42, 2, aZfs), "a"); err != nil {
			t.Fatal(err)
		}

		return v
	}

	// The semantics of rbind and bind, here, is identical.
	const expected = `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 42:0 / /a rw - (fstype) tmpfs-x rw
1  0 42:1 / /a rw - (fstype) tmpfs-y rw
2  1 42:2 / /a rw - (fstype) tmpfs-z rw
3  -1 42:2 / /b rw - (fstype) tmpfs-z rw
`

	t.Run("bind", func(t *testing.T) {
		v := makeVFS(t)

		if err := v.BindMount("a", "b"); err != nil {
			t.Fatal(err)
		}

		assertVFS(t, v, expected)
	})

	t.Run("rbind", func(t *testing.T) {
		v := makeVFS(t)

		if err := v.RecursiveBindMount("a", "b"); err != nil {
			t.Fatal(err)
		}

		assertVFS(t, v, expected)
	})
}

func TestMountLinkage(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"a": &fstest.MapFile{Mode: fs.ModeDir},
		"b": &fstest.MapFile{Mode: fs.ModeDir},
		"c": &fstest.MapFile{Mode: fs.ModeDir},
	})

	// Check if rootfs links look correct.
	if v.RootMount().Parent() != nil {
		t.Fatal("rootfs has a parent?")
	}

	// Mount /a and check links for rootfs and a.
	if err := v.Mount(fstest.MapFS{}, "a"); err != nil {
		t.Fatal(err)
	}
	a := v.FindMount(0)
	if a == nil || a.MountPoint() != "a" {
		t.Fatal("cannot find mount for /a")
	}

	if v.RootMount().Parent() != nil {
		t.Fatal("rootfs has a parent?")
	}

	if a.Parent() != v.RootMount() {
		t.Fatal("/a is not a parent of the root?")
	}

	// Mount /b and check links for rootfs, a and b.
	if err := v.Mount(fstest.MapFS{}, "b"); err != nil {
		t.Fatal(err)
	}
	b := v.FindMount(1)
	if b == nil || b.MountPoint() != "b" {
		t.Fatal("cannot find mount for /b")
	}

	if v.RootMount().Parent() != nil {
		t.Fatal("rootfs has a parent?")
	}

	if a.Parent() != v.RootMount() {
		t.Fatal("/a is not a parent of the root?")
	}

	if b.Parent() != v.RootMount() {
		t.Fatal("/b is not a parent of the root?")
	}

	// Mount /c and check links for rootfs, a, b and c.
	if err := v.Mount(fstest.MapFS{}, "c"); err != nil {
		t.Fatal(err)
	}
	c := v.FindMount(2)
	if c == nil || c.MountPoint() != "c" {
		t.Fatal("cannot find mount for /c")
	}

	if v.RootMount().Parent() != nil {
		t.Fatal("rootfs has a parent?")
	}

	if a.Parent() != v.RootMount() {
		t.Fatal("/a is not a parent of the root?")
	}

	if b.Parent() != v.RootMount() {
		t.Fatal("/b is not a parent of the root?")
	}

	if c.Parent() != v.RootMount() {
		t.Fatal("/c is not a parent of the root?")
	}

	// Unmount /a and re-check linkage.
	if err := v.Unmount("a"); err != nil {
		t.Fatal(err)
	}
	if v.RootMount().Parent() != nil {
		t.Fatal("rootfs has a parent?")
	}

	if a.Parent() != nil {
		t.Fatal("/a is not detached from parent?")
	}

	if b.Parent() != v.RootMount() {
		t.Fatal("/b is not a parent of the root?")
	}

	// Unmount /c and re-check linkage.
	if err := v.Unmount("c"); err != nil {
		t.Fatal(err)
	}
	if v.RootMount().Parent() != nil {
		t.Fatal("rootfs has a parent?")
	}

	if b.Parent() != v.RootMount() {
		t.Fatal("/b is not detached from parent?")
	}

	if c.Parent() != nil {
		t.Fatal("/c is not detached from parent?")
	}
}
