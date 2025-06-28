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

// NOTE: We are not using fstest.TestFS as that requires at least fs.FS,
// which includes Open, and our implementation explicitly panics as we
// don't need that functionality.

func TestVFS_Mount(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"home":       &fstest.MapFile{Mode: fs.ModeDir},
		"etc/passwd": &fstest.MapFile{},
	})

	t.Run("mount-on-file", func(t *testing.T) {
		t.Log("Mounting FS as etc/passwd")
		err := v.Mount(fstest.MapFS{}, "etc/passwd")
		if err == nil {
			t.Fatal("Unexpected success")
		}

		if err.Error() != "mount etc/passwd: not a directory" {
			t.Fatal("Unexpected error", err)
		}

		t.Log("Final state", v)
	})

	t.Run("mount-on-non-existing", func(t *testing.T) {
		t.Log("Mounting FS as var")
		err := v.Mount(fstest.MapFS{}, "var")
		if err == nil {
			t.Fatal("Unexpected success")
		}

		if err.Error() != "mount var: file does not exist" {
			t.Fatal("Unexpected error", err)
		}

		t.Log("Final state", v)
	})

	t.Run("mount-on-dir", func(t *testing.T) {
		t.Log("Checking home/user/.vimrc before mount")
		if _, err := v.Stat("home/user/.vimrc"); err == nil {
			t.Fatal("Expected home/user/.vimrc not to be visible yet")
		}

		t.Log("Mounting FS as home")
		home := fstest.MapFS{"user/.vimrc": &fstest.MapFile{}}
		if err := v.Mount(home, "home"); err != nil {
			t.Fatal(err)
		}
		t.Log("After mounting on home", v)

		t.Log("Checking home/user/.vimrc after mount")
		if _, err := v.Stat("home/user/.vimrc"); err != nil {
			t.Fatal(err)
		}

		t.Run("overmount", func(t *testing.T) {
			t.Log("Mounting another FS at home")
			home2 := fstest.MapFS{"user/.viminfo": &fstest.MapFile{}}
			if err := v.Mount(home2, "home"); err != nil {
				t.Fatal(err)
			}
			t.Log("After mounting on home, again", v)

			t.Log("Checking home/user/.viminfo after mount")
			if _, err := v.Stat("home/user/.viminfo"); err != nil {
				t.Fatal(err)
			}

			t.Log("Checking home/user/.vimrc after mount")
			if _, err := v.Stat("home/user/.vimrc"); err == nil {
				t.Fatal("Expected home/user/.vimrc to be shadowed by home overmount")
			}
		})
	})

	t.Run("mount-path-with-trailing-slash", func(t *testing.T) {
		emptyFS := fstest.MapFS{}

		if err := v.Mount(emptyFS, "home/"); err == nil {
			t.Fatal("Unexpected success")
		}

		t.Log("Final state", v)
	})
}

func TestVFS_BindMount(t *testing.T) {
	t.Run("non-existing-source", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"target": &fstest.MapFile{Mode: fs.ModeDir},
		})

		err := v.BindMount("source", "target")
		if err == nil {
			t.Fatal("Unexpected success")
		}

		if err.Error() != "bind-mount source: file does not exist" {
			t.Fatal("Unexpected error", err)
		}

		t.Log("Final state", v)
	})

	t.Run("non-existing-target", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"source": &fstest.MapFile{Mode: fs.ModeDir},
		})

		err := v.BindMount("source", "target")
		if err == nil {
			t.Fatal("Unexpected success")
		}

		if err.Error() != "bind-mount target: file does not exist" {
			t.Fatal("Unexpected error", err)
		}

		t.Log("Final state", v)
	})

	t.Run("directory", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"home":             &fstest.MapFile{Mode: fs.ModeDir, Sys: "home"},
			"home2":            &fstest.MapFile{Mode: fs.ModeDir, Sys: "home2"},
			"home/user/.vimrc": &fstest.MapFile{},
		})

		t.Log("Inspecting home2 before bind mounting home over it")
		fi, err := v.Stat("home2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of home2 before bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Bind-mounting home at home2")
		if err := v.BindMount("home", "home2"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mount home -> home2", v)

		t.Log("Inspecting home2 after bind mounting home over it")
		fi, err = v.Stat("home2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of home2 after bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Inspecting home2/user/.vimrc")
		if _, err := v.Stat("home2/user/.vimrc"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("source-path-with-trailing-slash", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"home":  &fstest.MapFile{Mode: fs.ModeDir, Sys: "home"},
			"home2": &fstest.MapFile{Mode: fs.ModeDir, Sys: "home2"},
		})

		if err := v.BindMount("home/", "home2"); err == nil {
			t.Fatal("Unexpected success")
		}
	})

	t.Run("target-path-with-trailing-slash", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"home":  &fstest.MapFile{Mode: fs.ModeDir, Sys: "home"},
			"home2": &fstest.MapFile{Mode: fs.ModeDir, Sys: "home2"},
		})

		if err := v.BindMount("home", "home2/"); err == nil {
			t.Fatal("Unexpected success")
		}
	})

	t.Run("mount-point-directory", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"home":        &fstest.MapFile{Mode: fs.ModeDir, Sys: "home"},
			"home2":       &fstest.MapFile{Mode: fs.ModeDir, Sys: "home2"},
			"user/.vimrc": &fstest.MapFile{},
		})

		t.Log("Mounting FS at home")
		if err := v.Mount(&fstest.MapFS{"user/.vimrc": &fstest.MapFile{}}, "home"); err != nil {
			t.Fatal(err)
		}

		t.Log("After mounting a fs at home", v)

		t.Log("Bind-mounting home at home2")
		if err := v.BindMount("home", "home2"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind mounting home -> home2", v)

		t.Log("Inspecting home2 after bind mounting home over it")
		fi, err := v.Stat("home2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of home2 after bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Inspecting home2")
		if _, err := v.Stat("home2"); err != nil {
			t.Fatal(err)
		}

		t.Log("Inspecting home2/user/.vimrc")
		if _, err := v.Stat("home2/user/.vimrc"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("false-directory", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"home":  &fstest.MapFile{Mode: fs.ModeDir, Sys: "home"},
			"home2": &fstest.MapFile{Mode: fs.ModeDir, Sys: "home2"},
		})

		t.Log("Inspecting home2 before bind-mounting home over itself")
		fi, err := v.Stat("home2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of home2 before bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Bind-mounting home over itself")
		if err := v.BindMount("home", "home"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind mounting home over itself", v)

		t.Log("Inspecting home2 after bind-mounting home over itself")
		fi, err = v.Stat("home2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of home2 after bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		if sys, ok := fi.Sys().(string); !ok || sys != "home2" {
			t.Fatal("Unexpected stat of home2", fi)
		}
	})

	t.Run("sub-directory", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"var/home/user": &fstest.MapFile{Mode: fs.ModeDir, Sys: "actually var/home/user"},
			"home/user":     &fstest.MapFile{Mode: fs.ModeDir},
			"root":          &fstest.MapFile{Mode: fs.ModeDir},
		})

		t.Log("Bind-mounting var/home/user at home/user")
		if err := v.BindMount("var/home/user", "home/user"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind mounting var/home/user -> home/user", v)

		t.Log("Bind-mounting home/user at root")
		if err := v.BindMount("home/user", "root"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind mounting home/user -> root", v)

		t.Log("Inspecting root after both bind mounts")
		fi, err := v.Stat("root")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of root after both bind-mounts is %v (directory %v)", fi.Sys(), fi.IsDir())

		if sys, ok := fi.Sys().(string); !ok || sys != "actually var/home/user" {
			t.Fatal("Unexpected Sys()", sys, ok)
		}
	})

	t.Run("file", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"README.txt": &fstest.MapFile{Sys: "actually README.txt"},
			"README.md":  &fstest.MapFile{Sys: "actually README.md"},
		})

		t.Log("Inspecting README.txt before the bind-mount")
		fi, err := v.Stat("README.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("README.txt before bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		if fi.Name() != "README.txt" {
			t.Fatal("Unexpected name of README.txt before the bind-mount", fi.Name())
		}

		t.Log("Bind-mounting README.md over README.txt")
		if err := v.BindMount("README.md", "README.txt"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind mounting README.md -> README.txt", v)

		t.Log("Inspecting README.txt after the bind-mount")
		fi, err = v.Stat("README.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("README.txt after bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		if fi.Name() != "README.txt" {
			t.Fatal("Unexpected name of README.txt", fi.Name())
		}

		t.Log("Inspecting README.md after the bind-mount")
		fi, err = v.Stat("README.md")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("README.md after bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		if fi.Name() != "README.md" {
			t.Fatal("Unexpected name of README.md", fi.Name())
		}
	})

	t.Run("mount-point-file", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"foo.txt":   &fstest.MapFile{Sys: "foo.txt"},
			"foo.txt.2": &fstest.MapFile{Sys: "foo.txt.2"},
			"home":      &fstest.MapFile{Mode: fs.ModeDir},
		})

		t.Log("Mounting FS at home")
		if err := v.Mount(fstest.MapFS{"user/.vimrc": &fstest.MapFile{}}, "home"); err != nil {
			t.Fatal(err)
		}

		t.Log("After mounting FS at home", v)

		t.Log("Inspecting home/user/.vimrc before the bind-mount")
		fi, err := v.Stat("home/user/.vimrc")
		if err != nil {
			t.Fatal(err)
		}
		if fi.Name() != ".vimrc" {
			t.Fatal("Unexpected name of .vimrc", fi.Name())
		}
		if fi.Sys() != nil {
			t.Fatal("Unexpected Sys of .vimrc", fi.Sys())
		}

		t.Log("Bind-mounting foo.txt at foo.txt.2")
		if err := v.BindMount("foo.txt", "foo.txt.2"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mounting foo.txt -> foo.txt.2", v)

		t.Log("Bind-mounting foo.txt.2 at home/user/.vimrc")
		if err := v.BindMount("foo.txt.2", "home/user/.vimrc"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mounting foo.txt.2 -> home/user/.vimrc", v)

		t.Log("Inspecting home/user/.vimrc after the bind-mount")
		fi, err = v.Stat("home/user/.vimrc")
		if err != nil {
			t.Fatal(err)
		}
		if fi.Name() != ".vimrc" {
			t.Fatal("Unexpected name of .vimrc", fi.Name())
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "foo.txt" {
			t.Fatal("Unexpected Sys of .vimrc", fi.Sys())
		}
	})

	t.Run("false-file", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"foo.txt":   &fstest.MapFile{Sys: "foo.txt"},
			"foo.txt.2": &fstest.MapFile{Sys: "foo.txt.2"},
		})

		t.Log("Inspecting foo.txt.2 before bind-mounting foo.txt over itself")
		fi, err := v.Stat("foo.txt.2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of foo.txt.2 before bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Bind-mounting foo.txt over itself")
		if err := v.BindMount("foo.txt", "foo.txt"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mounting foo.txt over itself", v)

		t.Log("Inspecting foo.txt.2 after bind mounting foo.txt over itself")
		fi, err = v.Stat("foo.txt.2")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of foo.txt.2 after bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		if sys, ok := fi.Sys().(string); !ok || sys != "foo.txt.2" {
			t.Fatal("Unexpected stat of foo.txt.2", fi)
		}
	})
	t.Run("sub-file", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"var/home/user/file.txt": &fstest.MapFile{Sys: "actually var/home/user/file.txt"},
			"home/user":              &fstest.MapFile{Mode: fs.ModeDir},
			"root/file.txt":          &fstest.MapFile{},
		})

		t.Log("Inspecting var/home/user/file.txt before all bind-mounts")
		fi, err := v.Stat("var/home/user/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of var/home/user/file.txt before all bind-mounts %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Bind-mounting var/home/user at home/user")
		if err := v.BindMount("var/home/user", "home/user"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mounting var/home/user -> home/user", v)

		t.Log("Inspecting home/user/file.txt after first bind-mount")
		fi, err = v.Stat("home/user/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of home/user/file.txt after first bind-mount is %v (directory %v)", fi.Sys(), fi.IsDir())

		t.Log("Bind-mounting home/user/file.txt at root/file.txt")
		if err := v.BindMount("home/user/file.txt", "root/file.txt"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mounting home/user/file.txt -> root/file.txt", v)

		t.Log("Inspecting root/file.txt after both bind-mounts")
		fi, err = v.Stat("root/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Stat of root/file.txt after both bind-mounts is %v (directory %v)", fi.Sys(), fi.IsDir())

		if sys, ok := fi.Sys().(string); !ok || sys != "actually var/home/user/file.txt" {
			t.Fatal("Unexpected Sys()", sys, ok)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"file": &fstest.MapFile{},
			"dir":  &fstest.MapFile{Mode: fs.ModeDir},
		})

		if err := v.BindMount("file", "dir"); err == nil {
			t.Fatal("Unexpected success")
		}
		if err := v.BindMount("dir", "file"); err == nil {
			t.Fatal("Unexpected success")
		}
	})

	t.Run("complex", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{
			"home":      &fstest.MapFile{Mode: fs.ModeDir, Sys: "actually home"},
			"home/user": &fstest.MapFile{Mode: fs.ModeDir, Sys: "actually home/user"},
			"home2":     &fstest.MapFile{Mode: fs.ModeDir, Sys: "actually home2"},
			"home/user/.vimrc": &fstest.MapFile{
				Data: []byte("colorscheme elflord\n"),
				Mode: 0o644,
			},
		})
		if _, err := v.Stat("home/user/.vimrc"); err != nil {
			t.Fatal(err)
		}

		if err := v.BindMount("home", "home2"); err != nil {
			t.Fatal(err)
		}

		t.Log("After bind-mounting home -> home2", v)

		if _, err := v.Stat("home/user/.vimrc"); err != nil {
			t.Fatal(err)
		}
		if _, err := v.Stat("home2/user/.vimrc"); err != nil {
			t.Fatal(err)
		}

		userFS := fstest.MapFS{".profile": &fstest.MapFile{}}
		if err := v.Mount(userFS, "home/user"); err != nil {
			t.Fatal(err)
		}

		t.Log("After mounting a FS at home/user", v)

		if _, err := v.Stat("home/user/.profile"); err != nil {
			t.Fatal(err)
		}
		if _, err := v.Stat("home2/user/.vimrc"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestVFS_RecursiveBindMount(t *testing.T) {
	makeVFS := func(t *testing.T) *vfs.VFS {
		t.Helper()
		v := vfs.NewVFS(fstest.MapFS{
			"file.txt": &fstest.MapFile{Sys: "really file.txt"},
			"a/dir-1":  &fstest.MapFile{Mode: fs.ModeDir},
			"a/dir-2":  &fstest.MapFile{Mode: fs.ModeDir},
			"b":        &fstest.MapFile{Mode: fs.ModeDir},
		})
		d1 := fstest.MapFS{"sub-1": &fstest.MapFile{Mode: fs.ModeDir}}
		if err := v.Mount(d1, "a/dir-1"); err != nil {
			t.Fatal(err)
		}
		d2 := fstest.MapFS{"sub-2": &fstest.MapFile{Mode: fs.ModeDir}}
		if err := v.Mount(d2, "a/dir-2"); err != nil {
			t.Fatal(err)
		}
		s1 := fstest.MapFS{"file.txt": &fstest.MapFile{}}
		if err := v.Mount(s1, "a/dir-1/sub-1"); err != nil {
			t.Fatal(err)
		}
		if err := v.BindMount("file.txt", "a/dir-1/sub-1/file.txt"); err != nil {
			t.Fatal(err)
		}
		return v
	}

	t.Run("smoke", func(t *testing.T) {
		v := makeVFS(t)
		t.Log("State before recursive bind mount", v)
		if err := v.RecursiveBindMount("a", "b"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after recursive bind mount", v)

		fiA, err := v.Stat("a/dir-1/sub-1/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		fiB, err := v.Stat("b/dir-1/sub-1/file.txt")
		if err != nil {
			t.Fatal(err)
		}

		if fiA.Sys() != fiB.Sys() {
			t.Fatal("not the same file")
		}
	})

	t.Run("subsmoke", func(t *testing.T) {
		v := makeVFS(t)
		t.Log("State before recursive bind mount", v)
		if err := v.RecursiveBindMount("a/dir-1", "b"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after recursive bind mount", v)

		fiA, err := v.Stat("a/dir-1/sub-1/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		fiB, err := v.Stat("b/sub-1/file.txt")
		if err != nil {
			t.Fatal(err)
		}

		if fiA.Sys() != fiB.Sys() {
			t.Fatal("not the same file")
		}
	})

	t.Run("shadowed", func(t *testing.T) {
		v := makeVFS(t)
		if err := v.Mount(fstest.MapFS{}, "a"); err != nil {
			t.Fatal(err)
		}
		t.Log("State before recursive bind mount", v)
		if err := v.RecursiveBindMount("a", "b"); err != nil {
			t.Fatal(err)
		}
		t.Log("State after recursive bind mount", v)

		if _, err := v.Stat("a/dir-1/sub-1/file.txt"); err == nil {
			t.Fatal("Unexpected succcess")
		}
		if _, err := v.Stat("b/dir-1/sub-1/file.txt"); err == nil {
			t.Fatal("Unexpected succcess")
		}
	})
}

func TestVFS_Unmount(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{
		"tmp":        &fstest.MapFile{Mode: fs.ModeDir, Sys: "tmp"},
		"run":        &fstest.MapFile{Mode: fs.ModeDir, Sys: "run"},
		"file.txt":   &fstest.MapFile{Sys: "file.txt"},
		"file.txt.2": &fstest.MapFile{Sys: "file.txt.2"},
	})

	t.Run("tmp-umounted", func(t *testing.T) {
		err := v.Unmount("tmp")
		if err == nil {
			t.Fatal("Unexpected success")
		}
		if err.Error() != "unmount tmp: not mounted" {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("rootfs", func(t *testing.T) {
		err := v.Unmount("")
		if err == nil {
			t.Fatal("Unexpected success")
		}
		if err.Error() != "unmount : mount is busy" {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("tmp-mounted", func(t *testing.T) {
		if err := v.Mount(fstest.MapFS{}, "tmp"); err != nil {
			t.Fatal(err)
		}

		if err := v.Unmount("tmp"); err != nil {
			t.Fatal(err)
		}

		t.Log("After mounting and unmounting FS at tmp", v)
	})

	t.Run("tmp-mounted-twice", func(t *testing.T) {
		t.Log("Mounting fs on tmp (the one with 'a')")
		if err := v.Mount(fstest.MapFS{"a": &fstest.MapFile{}}, "tmp"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: tmp/a is the file from the mounted fs")
		if _, err := v.Stat("tmp/a"); err != nil {
			t.Fatal(err)
		}

		t.Log("Mounting fs on tmp (the one with 'b')")
		if err := v.Mount(fstest.MapFS{"b": &fstest.MapFile{}}, "tmp"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: tmp/a is now shadowed by the new mount on tmp")
		if _, err := v.Stat("tmp/a"); err == nil {
			t.Fatal("Unexpected success")
		}

		t.Log("Confidence check: tmp/b is the file from the mounted fs")
		if _, err := v.Stat("tmp/b"); err != nil {
			t.Fatal(err)
		}

		t.Log("Unmounting top mount on tmp")
		if err := v.Unmount("tmp"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: tmp/a is no longer shadowed")
		if _, err := v.Stat("tmp/a"); err != nil {
			t.Fatal(err)
		}

		t.Log("Unmounting bottom mount on tmp")
		if err := v.Unmount("tmp"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: tmp/a is gone")
		if _, err := v.Stat("tmp/a"); err == nil {
			t.Fatal("Unexpected success")
		}

		t.Log("Final state", v)
	})

	t.Run("bind-mounted-dir", func(t *testing.T) {
		if err := v.BindMount("tmp", "run"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: run is really tmp")
		fi, err := v.Stat("run")
		if err != nil {
			t.Fatal(err)
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "tmp" {
			t.Fatal("Unexpected sys of run", fi.Sys())
		}

		if err := v.Unmount("run"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: run is back to itself")
		fi, err = v.Stat("run")
		if err != nil {
			t.Fatal(err)
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "run" {
			t.Fatal("Unexpected sys of run", fi.Sys())
		}

		t.Log("Final state", v)
	})

	t.Run("bind-mounted-file", func(t *testing.T) {
		if err := v.BindMount("file.txt", "file.txt.2"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: file.txt.2 is really file.txt")
		fi, err := v.Stat("file.txt.2")
		if err != nil {
			t.Fatal(err)
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "file.txt" {
			t.Fatal("Unexpected sys of file.txt.2", fi.Sys())
		}

		if err := v.Unmount("file.txt.2"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: file.txt.2 is back to itself")
		fi, err = v.Stat("file.txt.2")
		if err != nil {
			t.Fatal(err)
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "file.txt.2" {
			t.Fatal("Unexpected sys of file.txt.2", fi.Sys())
		}

		t.Log("Final state", v)
	})

	t.Run("recursive-bind-mounted-file", func(t *testing.T) {
		if err := v.RecursiveBindMount("file.txt", "file.txt.2"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: file.txt.2 is really file.txt")
		fi, err := v.Stat("file.txt.2")
		if err != nil {
			t.Fatal(err)
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "file.txt" {
			t.Fatal("Unexpected sys of file.txt.2", fi.Sys())
		}

		if err := v.Unmount("file.txt.2"); err != nil {
			t.Fatal(err)
		}

		t.Log("Confidence check: file.txt.2 is back to itself")
		fi, err = v.Stat("file.txt.2")
		if err != nil {
			t.Fatal(err)
		}
		if sys, ok := fi.Sys().(string); !ok || sys != "file.txt.2" {
			t.Fatal("Unexpected sys of file.txt.2", fi.Sys())
		}

		t.Log("Final state", v)
	})

	t.Run("mount-path-with-trailing-slash", func(t *testing.T) {
		if err := v.Unmount("home/"); err == nil {
			t.Fatal("Unexpected success")
		}
	})
}
