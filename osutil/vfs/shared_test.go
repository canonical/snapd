package vfs_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/snapcore/snapd/osutil/vfs"
)

func TestVFS_MakeShared(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{"home": &fstest.MapFile{Mode: fs.ModeDir}})
		assertSuccess(t, v.MakeShared(""))
		assertSuccess(t, v.Mount(fstest.MapFS{}, "home"))
		assertVFS(t, v, `
-1 -1 0:0 / / rw shared:1 - (fstype) (source) rw
0  -1 0:0 / /home rw shared:2 - (fstype) (source) rw
`)
	})

	t.Run("twice", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{})
		assertSuccess(t, v.MakeShared(""))
		assertSuccess(t, v.MakeShared(""))
		assertVFS(t, v, `
-1 -1 0:0 / / rw shared:1 - (fstype) (source) rw
`)
	})

	t.Run("not-mount-point", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{"home": &fstest.MapFile{Mode: fs.ModeDir}})
		assertErrorIs(t, v.MakeShared("home"), vfs.ErrNotMounted)
		assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
`)
	})
}

func TestVFS_RecursivelyMakeShared(t *testing.T) {
	t.Run("top-level", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{"home": &fstest.MapFile{Mode: fs.ModeDir}})
		assertSuccess(t, v.Mount(fstest.MapFS{"zyga": &fstest.MapFile{Mode: fs.ModeDir}}, "home"))
		assertSuccess(t, v.Mount(fstest.MapFS{}, "home/zyga"))
		assertSuccess(t, v.MakeRecursivelyShared(""))
		assertVFS(t, v, `
-1 -1 0:0 / / rw shared:1 - (fstype) (source) rw
0  -1 0:0 / /home rw shared:2 - (fstype) (source) rw
1  0 0:0 / /home/zyga rw shared:3 - (fstype) (source) rw
`)
	})

	t.Run("not-top-level", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{"home": &fstest.MapFile{Mode: fs.ModeDir}})
		assertSuccess(t, v.Mount(fstest.MapFS{"zyga": &fstest.MapFile{Mode: fs.ModeDir}}, "home"))
		assertSuccess(t, v.Mount(fstest.MapFS{}, "home/zyga"))
		assertSuccess(t, v.MakeRecursivelyShared("home"))
		assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /home rw shared:1 - (fstype) (source) rw
1  0 0:0 / /home/zyga rw shared:2 - (fstype) (source) rw
`)
	})

	t.Run("leaf", func(t *testing.T) {
		v := vfs.NewVFS(fstest.MapFS{"home": &fstest.MapFile{Mode: fs.ModeDir}})
		assertSuccess(t, v.Mount(fstest.MapFS{"zyga": &fstest.MapFile{Mode: fs.ModeDir}}, "home"))
		assertSuccess(t, v.Mount(fstest.MapFS{}, "home/zyga"))
		assertSuccess(t, v.MakeRecursivelyShared("home/zyga"))
		assertVFS(t, v, `
-1 -1 0:0 / / rw - (fstype) (source) rw
0  -1 0:0 / /home rw - (fstype) (source) rw
1  0 0:0 / /home/zyga rw shared:1 - (fstype) (source) rw
`)
	})
}
