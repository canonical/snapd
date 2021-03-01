// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package image_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func (s *imageSuite) TestDownloadpOptionsString(c *check.C) {
	tests := []struct {
		opts image.DownloadOptions
		str  string
	}{
		{image.DownloadOptions{LeavePartialOnError: true}, ""},
		{image.DownloadOptions{}, ""},
		{image.DownloadOptions{TargetDir: "/foo"}, `in "/foo"`},
		{image.DownloadOptions{Basename: "foo"}, `to "foo.snap"`},
		{image.DownloadOptions{Channel: "foo"}, `from channel "foo"`},
		{image.DownloadOptions{Revision: snap.R(42)}, `(42)`},
		{image.DownloadOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, `from cohort "…rStUvWxYz"`},
		{image.DownloadOptions{
			TargetDir: "/foo",
			Basename:  "bar",
			Channel:   "baz",
			Revision:  snap.R(13),
			CohortKey: "MSBIc3dwOW9PemozYjRtdzhnY0MwMFh0eFduS0g5UWlDUSAxNTU1NDExNDE1IDBjYzJhNTc1ZjNjOTQ3ZDEwMWE1NTNjZWFkNmFmZDE3ZWJhYTYyNjM4ZWQ3ZGMzNjI5YmU4YjQ3NzAwMjdlMDk=",
		}, `(13) from channel "baz" from cohort "…wMjdlMDk=" to "bar.snap" in "/foo"`}, // note this one is not 'valid' so it's ok if the string is a bit wonky

	}

	for _, t := range tests {
		c.Check(t.opts.String(), check.Equals, t.str)
	}
}

func (s *imageSuite) TestDownloadOptionsValid(c *check.C) {
	tests := []struct {
		opts image.DownloadOptions
		err  error
	}{
		{image.DownloadOptions{}, nil}, // might want to error if no targetdir
		{image.DownloadOptions{TargetDir: "foo"}, nil},
		{image.DownloadOptions{Channel: "foo"}, nil},
		{image.DownloadOptions{Revision: snap.R(42)}, nil},
		{image.DownloadOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, nil},
		{image.DownloadOptions{
			Channel:  "foo",
			Revision: snap.R(42),
		}, nil},
		{image.DownloadOptions{
			Channel:   "foo",
			CohortKey: "bar",
		}, nil},
		{image.DownloadOptions{
			Revision:  snap.R(1),
			CohortKey: "bar",
		}, image.ErrRevisionAndCohort},
		{image.DownloadOptions{
			Basename: "/foo",
		}, image.ErrPathInBase},
	}

	for _, t := range tests {
		t.opts.LeavePartialOnError = true
		c.Check(t.opts.Validate(), check.Equals, t.err)
		t.opts.LeavePartialOnError = false
		c.Check(t.opts.Validate(), check.Equals, t.err)
	}
}

func (s *imageSuite) TestDownloadSnap(c *check.C) {
	// TODO: maybe expand on this (test coverage of DownloadSnap is really bad)

	// env shenanigans
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	debug, hadDebug := os.LookupEnv("SNAPD_DEBUG")
	os.Setenv("SNAPD_DEBUG", "1")
	if hadDebug {
		defer os.Setenv("SNAPD_DEBUG", debug)
	} else {
		defer os.Unsetenv("SNAPD_DEBUG")
	}
	logbuf, restore := logger.MockLogger()
	defer restore()

	s.setupSnaps(c, map[string]string{
		"core": "canonical",
	}, "")

	dlDir := c.MkDir()
	opts := image.DownloadOptions{
		TargetDir: dlDir,
	}
	fn, info, redirectChannel, err := s.tsto.DownloadSnap("core", opts)
	c.Assert(err, check.IsNil)
	c.Check(fn, check.Matches, filepath.Join(dlDir, `core_\d+.snap`))
	c.Check(info.SnapName(), check.Equals, "core")
	c.Check(redirectChannel, check.Equals, "")

	c.Check(logbuf.String(), check.Matches, `.* DEBUG: Going to download snap "core" `+opts.String()+".\n")
}

var validGadgetYaml = `
volumes:
  vol1:
    bootloader: grub
    structure:
      - name: non-fs
        type: bare
        size: 512
        offset: 0
        content:
        - image: non-fs.img
      - name: ubuntu-seed
        role: system-seed
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
        filesystem: ext4
        content:
         - source: system-seed.efi
           target: EFI/boot/system-seed.efi
      - name: structure-name
        role: system-boot
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
        filesystem: ext4
        content:
         - source: grubx64.efi
           target: EFI/boot/grubx64.efi
      - name: ubuntu-data
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
  vol2:
    structure:
      - name: struct2
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
        filesystem: ext4
        content:
         - source: foo
           target: subdir/foo
`

func (s *imageSuite) TestWriteResolvedContent(c *check.C) {
	prepareImageDir := c.MkDir()

	s.testWriteResolvedContent(c, prepareImageDir)
}

func (s *imageSuite) TestWriteResolvedContentRelativePath(c *check.C) {
	prepareImageDir := c.MkDir()

	// chdir to prepareImage dir and run writeResolvedContent from
	// this relative dir
	cwd, err := os.Getwd()
	c.Assert(err, check.IsNil)
	err = os.Chdir(prepareImageDir)
	c.Assert(err, check.IsNil)
	defer func() { os.Chdir(cwd) }()

	s.testWriteResolvedContent(c, ".")
}

func (s *imageSuite) testWriteResolvedContent(c *check.C, prepareImageDir string) {
	// on uc20 there is a "system-seed" under the <PrepareImageDir>
	uc20systemSeed, err := filepath.Abs(filepath.Join(prepareImageDir, "system-seed"))
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(uc20systemSeed, 0755)
	c.Assert(err, check.IsNil)

	// the resolved content is written here
	gadgetRoot := c.MkDir()
	snaptest.PopulateDir(gadgetRoot, [][]string{
		{"meta/snap.yaml", packageGadget},
		{"meta/gadget.yaml", validGadgetYaml},
		{"system-seed.efi", "content of system-seed.efi"},
		{"grubx64.efi", "content of grubx64.efi"},
		{"foo", "content of foo"},
		{"non-fs.img", "content of non-fs.img"},
	})
	kernelRoot := c.MkDir()
	model := s.makeUC20Model(nil)

	err = image.WriteResolvedContent(prepareImageDir, gadgetRoot, kernelRoot, model)
	c.Assert(err, check.IsNil)

	// XXX: add testutil.DirEquals([][]string)
	cmd := exec.Command("find", ".", "-printf", "%y %P\n")
	cmd.Dir = prepareImageDir
	tree, err := cmd.CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(tree), check.Equals, `d 
d system-seed
d system-seed/EFI
d system-seed/EFI/boot
f system-seed/EFI/boot/system-seed.efi
d resolved-content
d resolved-content/vol1
d resolved-content/vol1/structure-name
d resolved-content/vol1/structure-name/EFI
d resolved-content/vol1/structure-name/EFI/boot
f resolved-content/vol1/structure-name/EFI/boot/grubx64.efi
l resolved-content/vol1/ubuntu-seed
d resolved-content/vol2
d resolved-content/vol2/struct2
d resolved-content/vol2/struct2/subdir
f resolved-content/vol2/struct2/subdir/foo
`)
	// check symlink target for "ubuntu-seed" -> <prepareImageDir>/system-seed
	t, err := os.Readlink(filepath.Join(prepareImageDir, "resolved-content/vol1/ubuntu-seed"))
	c.Assert(err, check.IsNil)
	c.Check(t, check.Equals, uc20systemSeed)
}
