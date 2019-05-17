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
package gadget_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
)

type rawTestSuite struct {
	dir    string
	backup string
}

var _ = Suite(&rawTestSuite{})

func (r *rawTestSuite) SetUpTest(c *C) {
	r.dir = c.MkDir()
	r.backup = c.MkDir()
}

func openSizedFile(c *C, path string, size gadget.Size) *os.File {
	f, err := os.Create(path)
	c.Assert(err, IsNil)

	if size != 0 {
		err = f.Truncate(int64(size))
		c.Assert(err, IsNil)
	}

	return f
}

type mutateWrite struct {
	what []byte
	off  int64
}

func mutateFile(c *C, path string, size gadget.Size, writes []mutateWrite) {
	out := openSizedFile(c, path, size)
	for _, op := range writes {
		_, err := out.WriteAt(op.what, op.off)
		c.Assert(err, IsNil)
	}
}

func (r *rawTestSuite) TestRawWriterHappy(c *C) {

	rw := gadget.NewRawStructureWriter(r.dir)

	out := openSizedFile(c, filepath.Join(r.dir, "out.img"), 2048)
	defer out.Close()

	makeSizedFile(c, filepath.Join(r.dir, "foo.img"), 128, []byte("foo foo foo"))
	makeSizedFile(c, filepath.Join(r.dir, "bar.img"), 128, []byte("bar bar bar"))
	err := rw.Write(out, &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 0,
				Size:        128,
			}, {
				VolumeContent: &gadget.VolumeContent{
					Image: "bar.img",
				},
				StartOffset: 1024,
				Size:        128,
			},
		},
	})
	c.Assert(err, IsNil)

	expectedPath := filepath.Join(r.dir, "expected.img")
	mutateFile(c, expectedPath, 2048, []mutateWrite{
		{[]byte("foo foo foo"), 0},
		{[]byte("bar bar bar"), 1024},
	})
	expected, err := os.Open(expectedPath)
	c.Assert(err, IsNil)
	defer expected.Close()

	// rewind
	_, err = out.Seek(0, io.SeekStart)
	c.Assert(err, IsNil)
	_, err = expected.Seek(0, io.SeekStart)
	c.Assert(err, IsNil)

	c.Check(osutil.StreamsEqual(out, expected), Equals, true)
}

func (r *rawTestSuite) TestRawWriterNoFile(c *C) {

	rw := gadget.NewRawStructureWriter(r.dir)

	out := openSizedFile(c, filepath.Join(r.dir, "out.img"), 2048)
	defer out.Close()

	err := rw.Write(out, &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 0,
			},
		},
	})
	c.Assert(err, ErrorMatches, "failed to write image.* cannot open image file:.* no such file or directory")
}

type mockWriteSeeker struct {
	write func(b []byte) (n int, err error)
	seek  func(offset int64, whence int) (ret int64, err error)
}

func (m *mockWriteSeeker) Write(b []byte) (n int, err error) {
	if m.write != nil {
		return m.write(b)
	}
	return len(b), nil
}

func (m *mockWriteSeeker) Seek(offset int64, whence int) (ret int64, err error) {
	if m.seek != nil {
		return m.seek(offset, whence)
	}
	return offset, nil
}

func (r *rawTestSuite) TestRawWriterFailInWriteSeeker(c *C) {
	rw := gadget.NewRawStructureWriter(r.dir)

	out := &mockWriteSeeker{
		write: func(b []byte) (n int, err error) {
			c.Logf("write write\n")
			return 0, errors.New("failed")
		},
	}
	makeSizedFile(c, filepath.Join(r.dir, "foo.img"), 128, []byte("foo foo foo"))

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 1024,
				Size:        128,
			},
		},
	}
	err := rw.Write(out, ps)
	c.Assert(err, ErrorMatches, "failed to write image .*: cannot write image: failed")

	out = &mockWriteSeeker{
		seek: func(offset int64, whence int) (ret int64, err error) {
			return 0, errors.New("failed")
		},
	}
	err = rw.Write(out, ps)
	c.Assert(err, ErrorMatches, "failed to write image .*: cannot seek to content start offset 0x400: failed")
}

func (r *rawTestSuite) TestRawWriterFailWithNonBare(c *C) {
	rw := gadget.NewRawStructureWriter(r.dir)

	out := &mockWriteSeeker{}

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			// non-bare
			Filesystem: "ext4",
		},
	}
	err := rw.Write(out, ps)
	c.Assert(err, ErrorMatches, "structure #0 is not bare")
}

func getFileSize(c *C, path string) int64 {
	stat, err := os.Stat(path)
	c.Assert(err, IsNil)
	return stat.Size()
}

func (r *rawTestSuite) TestRawUpdaterBackupUpdateRestoreSame(c *C) {

	diskPath := filepath.Join(r.dir, "disk.img")
	mutateFile(c, diskPath, 2048, []mutateWrite{
		{[]byte("foo foo foo"), 0},
		{[]byte("bar bar bar"), 1024},
	})

	makeSizedFile(c, filepath.Join(r.dir, "foo.img"), 128, []byte("foo foo foo"))
	makeSizedFile(c, filepath.Join(r.dir, "bar.img"), 128, []byte("bar bar bar"))
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 0,
				Size:        128,
			}, {
				VolumeContent: &gadget.VolumeContent{
					Image: "bar.img",
				},
				StartOffset: 1024,
				Size:        128,
			},
		},
	}
	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return diskPath, nil
	})

	err := ru.Backup(nil, ps)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0)+".same"), Equals, true)
	c.Check(osutil.FileExists(ru.ContentBackupPath(ps, &ps.PositionedContent[1], 1)+".same"), Equals, true)

	emptyDiskPath := filepath.Join(r.dir, "disk-not-written.img")
	err = osutil.AtomicWriteFile(emptyDiskPath, nil, 0644, 0)
	c.Assert(err, IsNil)
	// update should be a noop now, use the same locations, point to a file
	// of 0 size, so that seek fails and write would increase the size
	ru = gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		return emptyDiskPath, nil
	})
	err = ru.Update(nil, ps)
	c.Assert(err, IsNil)
	c.Check(getFileSize(c, emptyDiskPath), Equals, int64(0))

	// rollback also is a noop
	err = ru.Rollback(nil, ps)
	c.Assert(err, IsNil)
	c.Check(getFileSize(c, emptyDiskPath), Equals, int64(0))
}

func (r *rawTestSuite) TestRawUpdaterBackupUpdateRestoreDifferent(c *C) {

	diskPath := filepath.Join(r.dir, "disk.img")
	mutateFile(c, diskPath, 2048, []mutateWrite{
		{[]byte("foo foo foo"), 0},
		{[]byte("bar bar bar"), 1024},
	})

	pristinePath := filepath.Join(r.dir, "pristine.img")
	err := osutil.CopyFile(diskPath, pristinePath, 0)
	c.Assert(err, IsNil)

	expectedPath := filepath.Join(r.dir, "expected.img")
	mutateFile(c, expectedPath, 2048, []mutateWrite{
		{[]byte("zzz zzz zzz zzz"), 0},
		{[]byte("xxx xxx xxx xxx"), 1024},
	})

	makeSizedFile(c, filepath.Join(r.dir, "foo.img"), 128, []byte("zzz zzz zzz zzz"))
	makeSizedFile(c, filepath.Join(r.dir, "bar.img"), 256, []byte("xxx xxx xxx xxx"))
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 0,
				Size:        128,
			}, {
				VolumeContent: &gadget.VolumeContent{
					Image: "bar.img",
				},
				StartOffset: 1024,
				Size:        256,
			},
		},
	}
	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return diskPath, nil
	})

	err = ru.Backup(nil, ps)
	c.Assert(err, IsNil)

	for _, e := range []struct {
		path   string
		size   int64
		exists bool
	}{
		{ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0) + ".backup", 128, true},
		{ru.ContentBackupPath(ps, &ps.PositionedContent[1], 1) + ".backup", 256, true},
		{ru.ContentBackupPath(ps, &ps.PositionedContent[1], 1) + ".same", 0, false},
		{ru.ContentBackupPath(ps, &ps.PositionedContent[1], 1) + ".same", 0, false},
	} {
		c.Check(osutil.FileExists(e.path), Equals, e.exists)
		if e.exists {
			c.Check(getFileSize(c, e.path), Equals, e.size)
		}
	}

	err = ru.Update(nil, ps)
	c.Assert(err, IsNil)

	// after update, files should be identical
	c.Check(osutil.FilesAreEqual(diskPath, expectedPath), Equals, true)

	// rollback restores the original contents
	err = ru.Rollback(nil, ps)
	c.Assert(err, IsNil)

	// which should match the pristine copy now
	c.Check(osutil.FilesAreEqual(diskPath, pristinePath), Equals, true)
}

func (r *rawTestSuite) TestRawUpdaterBackupErrors(c *C) {
	diskPath := filepath.Join(r.dir, "disk.img")
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 128,
				Size:        128,
			},
		},
	}

	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return diskPath, nil
	})

	err := ru.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot open device for reading: .*")
	c.Check(osutil.FileExists(ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0)+".backup"), Equals, false)

	// 0 sized disk, copying will fail with early EOF
	makeSizedFile(c, diskPath, 0, nil)

	err = ru.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot backup image .*: cannot backup original image: EOF")
	c.Check(osutil.FileExists(ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0)+".backup"), Equals, false)

	// make proper disk image now
	err = os.Remove(diskPath)
	c.Assert(err, IsNil)
	makeSizedFile(c, diskPath, 2048, nil)

	err = ru.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot backup image .*: cannot checksum update image: .*")
	c.Check(osutil.FileExists(ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0)+".backup"), Equals, false)
}

func (r *rawTestSuite) TestRawUpdaterBackupIdempotent(c *C) {
	diskPath := filepath.Join(r.dir, "disk.img")
	// 0 sized disk, copying will fail with early EOF
	makeSizedFile(c, diskPath, 0, nil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 128,
				Size:        128,
			},
		},
	}

	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return diskPath, nil
	})

	contentBackupBasePath := ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0)
	// mock content backed-up marker
	makeSizedFile(c, contentBackupBasePath+".backup", 0, nil)

	// never reached copy, hence no error
	err := ru.Backup(nil, ps)
	c.Assert(err, IsNil)

	err = os.Remove(contentBackupBasePath + ".backup")
	c.Assert(err, IsNil)

	// mock content is-identical marker
	makeSizedFile(c, contentBackupBasePath+".same", 0, nil)
	// never reached copy, hence no error
	err = ru.Backup(nil, ps)
	c.Assert(err, IsNil)
}

func (r *rawTestSuite) TestRawUpdaterFindDeviceFailed(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 128,
				Size:        128,
			},
		},
	}

	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, nil)
	err := ru.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot find device matching structure #0: device lookup not implemented")

	ru = gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return "", errors.New("failed")
	})
	err = ru.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot find device matching structure #0: failed")

	err = ru.Update(nil, ps)
	c.Assert(err, ErrorMatches, "cannot find device matching structure #0: failed")

	err = ru.Rollback(nil, ps)
	c.Assert(err, ErrorMatches, "cannot find device matching structure #0: failed")
}

func (r *rawTestSuite) TestRawUpdaterRollbackErrors(c *C) {
	diskPath := filepath.Join(r.dir, "disk.img")
	// 0 sized disk, copying will fail with early EOF
	makeSizedFile(c, diskPath, 0, nil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 128,
				Size:        128,
			},
		},
	}

	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return diskPath, nil
	})

	err := ru.Rollback(nil, ps)
	c.Assert(err, ErrorMatches, "cannot rollback image foo.img.*: cannot open backup image: .*no such file or directory")

	contentBackupPath := ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0) + ".backup"

	// trigger short read
	makeSizedFile(c, contentBackupPath, 0, nil)

	err = ru.Rollback(nil, ps)
	c.Assert(err, ErrorMatches, "cannot rollback image foo.img.*: cannot restore backup: cannot write image: EOF")

	// pretend device cannot be opened for writing
	err = os.Chmod(diskPath, 0000)
	c.Assert(err, IsNil)
	err = ru.Rollback(nil, ps)
	c.Assert(err, ErrorMatches, "cannot open device for writing: .* permission denied")
}

func (r *rawTestSuite) TestRawUpdaterUpdateErrors(c *C) {
	diskPath := filepath.Join(r.dir, "disk.img")
	// 0 sized disk, copying will fail with early EOF
	makeSizedFile(c, diskPath, 2048, nil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
		},
		StartOffset: 0,
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				StartOffset: 128,
				Size:        128,
			},
		},
	}

	ru := gadget.NewRawStructureUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return diskPath, nil
	})

	// backup/analysis not performed
	err := ru.Update(nil, ps)
	c.Assert(err, ErrorMatches, "cannot update image foo.img.*: missing backup file")

	// pretend backup was done
	makeSizedFile(c, ru.ContentBackupPath(ps, &ps.PositionedContent[0], 0)+".backup", 0, nil)

	err = ru.Update(nil, ps)
	c.Assert(err, ErrorMatches, "cannot update image foo.img.*: cannot open image file: .*no such file or directory")

	makeSizedFile(c, filepath.Join(r.dir, "foo.img"), 0, nil)
	err = ru.Update(nil, ps)
	c.Assert(err, ErrorMatches, "cannot update image foo.img.*: cannot write image: EOF")

	// pretend device cannot be opened for writing
	err = os.Chmod(diskPath, 0000)
	c.Assert(err, IsNil)
	err = ru.Update(nil, ps)
	c.Assert(err, ErrorMatches, "cannot open device for writing: .* permission denied")
}
