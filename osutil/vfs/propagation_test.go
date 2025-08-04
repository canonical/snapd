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

func TestVFS_PropagationToShared(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"a": &fstest.MapFile{Mode: fs.ModeDir},
		"b": &fstest.MapFile{Mode: fs.ModeDir},
		"c": &fstest.MapFile{Mode: fs.ModeDir},
	})
	must(t, v.Mount(fstest.MapFS{"1": &fstest.MapFile{Mode: fs.ModeDir}}, "a"))
	must(t, v.MakeShared("a"))
	must(t, v.BindMount("a", "b"))
	must(t, v.BindMount("a", "c"))
	must(t, v.Mount(fstest.MapFS{}, "a/1"))
	// The mount a/1 is propagated to a, b and c.
	assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /a rw shared:1 - (fstype) (source) rw
1  -1 0:0 / /b rw shared:1 - (fstype) (source) rw
2  -1 0:0 / /c rw shared:1 - (fstype) (source) rw
3  0 0:0 / /a/1 rw shared:2 - (fstype) (source) rw
4  1 0:0 / /b/1 rw shared:2 - (fstype) (source) rw
5  2 0:0 / /c/1 rw shared:2 - (fstype) (source) rw
`)
}

func TestVFS_PropagationToSlave(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"a": &fstest.MapFile{Mode: fs.ModeDir},
		"b": &fstest.MapFile{Mode: fs.ModeDir},
		"c": &fstest.MapFile{Mode: fs.ModeDir},
	})
	must(t, v.Mount(fstest.MapFS{"1": &fstest.MapFile{Mode: fs.ModeDir}}, "a"))
	must(t, v.MakeShared("a"))
	must(t, v.BindMount("a", "b"))
	must(t, v.MakeSlave("b"))
	must(t, v.BindMount("a", "c"))
	must(t, v.MakeSlave("c"))
	must(t, v.Mount(fstest.MapFS{}, "a/1"))
	// The mount a/1 is propagated to a, b and c.
	assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /a rw shared:1 - (fstype) (source) rw
1  -1 0:0 / /b rw master:1 - (fstype) (source) rw
2  -1 0:0 / /c rw master:1 - (fstype) (source) rw
3  0 0:0 / /a/1 rw shared:2 - (fstype) (source) rw
4  1 0:0 / /b/1 rw master:2 - (fstype) (source) rw
5  2 0:0 / /c/1 rw master:2 - (fstype) (source) rw
`)
}

func TestVFS_PropagationToSharedSlave(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"a": &fstest.MapFile{Mode: fs.ModeDir},
		"b": &fstest.MapFile{Mode: fs.ModeDir},
		"c": &fstest.MapFile{Mode: fs.ModeDir},
		"d": &fstest.MapFile{Mode: fs.ModeDir},
	})
	v.SetLastGroupID(41)
	must(t, v.Mount(WithSource("tmpfs-a", fstest.MapFS{
		"1": &fstest.MapFile{Mode: fs.ModeDir},
	}), "a"))
	must(t, v.MakeShared("a"))
	must(t, v.BindMount("a", "b"))
	must(t, v.MakeSlave("b"))
	must(t, v.MakeShared("b"))
	must(t, v.BindMount("a", "c"))
	must(t, v.MakeSlave("c"))
	must(t, v.MakeShared("c"))
	must(t, v.BindMount("c", "d"))
	must(t, v.Mount(WithSource("tmpfs-a-1", fstest.MapFS{}), "a/1"))
	// The mount a/1 is propagated to a, b, c and d.
	// FIXME: This test is subtly different to what the kernel really does
	assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /a rw shared:42 - (fstype) tmpfs-a rw
1  -1 0:0 / /b rw shared:43 master:42 - (fstype) tmpfs-a rw
2  -1 0:0 / /c rw shared:44 master:42 - (fstype) tmpfs-a rw
3  -1 0:0 / /d rw shared:44 master:42 - (fstype) tmpfs-a rw
4  0 0:0 / /a/1 rw shared:45 - (fstype) tmpfs-a-1 rw
5  1 0:0 / /b/1 rw shared:46 master:45 - (fstype) tmpfs-a-1 rw
6  2 0:0 / /c/1 rw shared:47 master:45 - (fstype) tmpfs-a-1 rw
7  3 0:0 / /d/1 rw shared:48 master:45 - (fstype) tmpfs-a-1 rw
`)
}

func TestVFS_PropagationAndSubDirectories(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"a": &fstest.MapFile{Mode: fs.ModeDir},
		"b": &fstest.MapFile{Mode: fs.ModeDir},
	})
	must(t, v.MakeShared(""))
	must(t, v.Mount(WithSource("tmpfs-a", fstest.MapFS{
		"1": &fstest.MapFile{Mode: fs.ModeDir},
		"2": &fstest.MapFile{Mode: fs.ModeDir},
	}), "a"))
	must(t, v.BindMount("a/1", "b"))
	must(t, v.Mount(WithSource("tmpfs-a-1", fstest.MapFS{}), "a/1"))
	must(t, v.Mount(WithSource("tmpfs-a-2", fstest.MapFS{}), "a/2"))
	assertVFS(t, v, `
-1 -1 0:0 / / rw shared:1 - (fstype) (source) rw
0  -1 0:0 / /a rw shared:2 - (fstype) tmpfs-a rw
1  -1 0:0 /1 /b rw shared:2 - (fstype) tmpfs-a rw
2  0 0:0 / /a/1 rw shared:3 - (fstype) tmpfs-a-1 rw
3  1 0:0 / /b rw shared:3 - (fstype) tmpfs-a-1 rw
4  0 0:0 / /a/2 rw shared:4 - (fstype) tmpfs-a-2 rw
`)
}

func TestVFS_MakeShared(t *testing.T) {
	t.Run("bind-keeps-sharing", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"a":       &fstest.MapFile{Mode: fs.ModeDir},
			"a_prime": &fstest.MapFile{Mode: fs.ModeDir},
		})
		must(t, v.Mount(fstest.MapFS{"b": &fstest.MapFile{Mode: fs.ModeDir}}, "a"))
		must(t, v.MakeShared("a"))
		must(t, v.Mount(fstest.MapFS{}, "a/b"))
		must(t, v.RecursiveBindMount("a", "a_prime"))
		assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /a rw shared:1 - (fstype) (source) rw
1  0 0:0 / /a/b rw shared:2 - (fstype) (source) rw
2  -1 0:0 / /a_prime rw shared:1 - (fstype) (source) rw
3  2 0:0 / /a_prime/b rw shared:2 - (fstype) (source) rw
`)
	})

	t.Run("share-propagates", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"a":       &fstest.MapFile{Mode: fs.ModeDir},
			"a_prime": &fstest.MapFile{Mode: fs.ModeDir},
		})
		must(t, v.Mount(fstest.MapFS{"b": &fstest.MapFile{Mode: fs.ModeDir}}, "a"))
		must(t, v.MakeShared("a"))
		must(t, v.RecursiveBindMount("a", "a_prime"))
		must(t, v.Mount(fstest.MapFS{}, "a/b"))
		assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /a rw shared:1 - (fstype) (source) rw
1  -1 0:0 / /a_prime rw shared:1 - (fstype) (source) rw
2  0 0:0 / /a/b rw shared:2 - (fstype) (source) rw
3  1 0:0 / /a_prime/b rw shared:2 - (fstype) (source) rw
`)
	})
}
