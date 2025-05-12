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

type testL struct{ t *testing.T }

func (tl testL) Log(args ...any) {
	tl.t.Helper()
	tl.t.Log(args...)
}

func (tl testL) Logf(format string, args ...any) {
	tl.t.Helper()
	tl.t.Logf(format, args...)
}

type MajorMinorMapFS struct {
	fstest.MapFS
	Major int
	Minor int
}

func (fs MajorMinorMapFS) MajorMinor() (int, int) {
	return fs.Major, fs.Minor
}

func TestVFS_MakeShared(t *testing.T) {
	t.Run("bind-keeps-sharing", func(t *testing.T) {
		var events []vfs.Event
		// Initial state.
		// d--- /           (rootfs)
		// d--- /a
		// d--- /a_prime
		v := vfs.NewVFS(fstest.MapFS{
			"a":       &fstest.MapFile{Mode: fs.ModeDir},
			"a_prime": &fstest.MapFile{Mode: fs.ModeDir},
		})
		t.Log("Initial state of the VFS", v)

		v.SetObserver(func(e vfs.Event) { events = append(events, e) })
		v.SetLogger(testL{t})

		// Mount fs on /a. The new fs has a single directory "b".
		// d--- /           (rootfs)
		// d--- /a 			(mount point)
		// d--- /a/b
		// d--- /a_prime
		if err := v.Mount(fstest.MapFS{"b": &fstest.MapFile{Mode: fs.ModeDir}}, "a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after mounting /a", v)

		// Make /a shared:
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b
		// d--- /a_prime
		if err := v.MakeShared("a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after making /a shared", v)

		// Mount fs on /a/b. The new fs is empty.
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b        (mount point, shared:2)
		// d--- /a_prime
		if err := v.Mount(fstest.MapFS{}, "a/b"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after mounting /a/b", v)

		// Recursive bind /a to /a_prime.
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b        (mount point, shared:2)
		// d--- /a_prime    (mount point, shared:1)
		// d--- /a_prime/b  (mount point, shared:2)
		if err := v.RecursiveBindMount("a", "a_prime"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after rbind /a -> /a_prime", v)
		// TODO: add a way to observe shared:N in tests.
		//
		t.Log("All events")
		for _, ev := range events {
			t.Logf(" - %#+v", ev)
		}
	})

	t.Run("share-propagates", func(t *testing.T) {
		var events []vfs.Event
		// Initial state.
		// d--- /           (rootfs)
		// d--- /a
		// d--- /a_prime
		v := vfs.NewVFS(MajorMinorMapFS{
			Major: 42,
			Minor: 1,
			MapFS: fstest.MapFS{
				"a":       &fstest.MapFile{Mode: fs.ModeDir},
				"a_prime": &fstest.MapFile{Mode: fs.ModeDir},
			},
		})
		t.Log("Initial state of the VFS", v)

		v.SetObserver(func(e vfs.Event) { events = append(events, e) })
		v.SetLogger(testL{t})

		// Mount fs on /a. The new fs has a single directory "b".
		// d--- /           (rootfs)
		// d--- /a 			(mount point)
		// d--- /a/b
		// d--- /a_prime
		if err := v.Mount(MajorMinorMapFS{
			Major: 42,
			Minor: 2,
			MapFS: fstest.MapFS{"b": &fstest.MapFile{Mode: fs.ModeDir}},
		}, "a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after mounting /a", v)

		// Make /a shared:
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b
		// d--- /a_prime
		if err := v.MakeShared("a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after making /a shared", v)

		// Recursive bind /a to /a_prime.
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b        (mount point, shared:2)
		// d--- /a_prime    (mount point, shared:1)
		// d--- /a_prime/b  (mount point, shared:2)
		if err := v.RecursiveBindMount("a", "a_prime"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after rbind /a -> /a_prime", v)
		// TODO: add a way to observe shared:N in tests.
		//

		// Mount fs on /a/b. The new fs is empty.
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b        (mount point, shared:2)
		// d--- /a_prime
		if err := v.Mount(MajorMinorMapFS{
			Major: 42,
			Minor: 3,
			MapFS: fstest.MapFS{},
		}, "a/b"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after mounting /a/b", v)

		t.Log("All events")
		for _, ev := range events {
			t.Logf(" - %#+v", ev)
		}
	})

	t.Run("share-back-propagates", func(t *testing.T) {
		var events []vfs.Event
		// Initial state.
		// d--- /           (rootfs)
		// d--- /a
		// d--- /a_prime
		v := vfs.NewVFS(MajorMinorMapFS{
			Major: 42,
			Minor: 1,
			MapFS: fstest.MapFS{
				"a":       &fstest.MapFile{Mode: fs.ModeDir},
				"a_prime": &fstest.MapFile{Mode: fs.ModeDir},
			},
		})
		t.Log("Initial state of the VFS", v)

		v.SetObserver(func(e vfs.Event) { events = append(events, e) })
		v.SetLogger(testL{t})

		// Mount fs on /a. The new fs has a single directory "b".
		// d--- /           (rootfs)
		// d--- /a 			(mount point)
		// d--- /a/b
		// d--- /a_prime
		if err := v.Mount(MajorMinorMapFS{
			Major: 42,
			Minor: 2,
			MapFS: fstest.MapFS{"b": &fstest.MapFile{Mode: fs.ModeDir}},
		}, "a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after mounting /a", v)

		// Make /a shared:
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b
		// d--- /a_prime
		if err := v.MakeShared("a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after making /a shared", v)

		// Recursive bind /a to /a_prime.
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b        (mount point, shared:2)
		// d--- /a_prime    (mount point, shared:1)
		// d--- /a_prime/b  (mount point, shared:2)
		if err := v.RecursiveBindMount("a", "a_prime"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after rbind /a -> /a_prime", v)
		// TODO: add a way to observe shared:N in tests.

		// Mount fs on /a_prime/b. The new fs is empty.
		// d--- /           (rootfs)
		// d--- /a 			(mount point, shared:1)
		// d--- /a/b        (mount point, shared:2)
		// d--- /a_prime
		if err := v.Mount(MajorMinorMapFS{
			Major: 42,
			Minor: 3,
			MapFS: fstest.MapFS{},
		}, "a_prime/b"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after mounting /a_prime/b", v)

		t.Log("All events")
		for _, ev := range events {
			t.Logf(" - %#+v", ev)
		}
	})
}

func TestVFS_MakeUnbindable(t *testing.T) {
	v := vfs.NewVFS(MajorMinorMapFS{
		Major: 42,
		Minor: 1,
		MapFS: fstest.MapFS{
			"a":       &fstest.MapFile{Mode: fs.ModeDir},
			"a_prime": &fstest.MapFile{Mode: fs.ModeDir},
		},
	})
	t.Log("Initial state of the VFS", v)

	// Mount fs on /a. The new fs has a single directory "b".
	if err := v.Mount(MajorMinorMapFS{
		Major: 42,
		Minor: 2,
		MapFS: fstest.MapFS{"b": &fstest.MapFile{Mode: fs.ModeDir}},
	}, "a"); err != nil {
		t.Fatal(err)
	}
	t.Log("State after mounting /a", v)

	// Make /a unbindable:
	if err := v.MakeUnbindable("a"); err != nil {
		t.Fatal(err)
	}
	t.Log("State after making /a unbindable", v)

	// Recursive bind /a to /a_prime.
	err := v.RecursiveBindMount("a", "a_prime")
	if err == nil {
		t.Fatal("Unexpected success, a is unbindable and bind-mount should have failed")
	}
	t.Log("State after rbind /a -> /a_prime", v)
}
