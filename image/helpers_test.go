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
	"sort"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func (s *imageSuite) TestDownloadpOptionsString(c *check.C) {
	tests := []struct {
		opts image.DownloadSnapOptions
		str  string
	}{
		{image.DownloadSnapOptions{LeavePartialOnError: true}, ""},
		{image.DownloadSnapOptions{}, ""},
		{image.DownloadSnapOptions{TargetDir: "/foo"}, `in "/foo"`},
		{image.DownloadSnapOptions{Basename: "foo"}, `to "foo.snap"`},
		{image.DownloadSnapOptions{Channel: "foo"}, `from channel "foo"`},
		{image.DownloadSnapOptions{Revision: snap.R(42)}, `(42)`},
		{image.DownloadSnapOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, `from cohort "…rStUvWxYz"`},
		{image.DownloadSnapOptions{
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

func (s *imageSuite) TestDownloadSnapOptionsValid(c *check.C) {
	tests := []struct {
		opts image.DownloadSnapOptions
		err  error
	}{
		{image.DownloadSnapOptions{}, nil}, // might want to error if no targetdir
		{image.DownloadSnapOptions{TargetDir: "foo"}, nil},
		{image.DownloadSnapOptions{Channel: "foo"}, nil},
		{image.DownloadSnapOptions{Revision: snap.R(42)}, nil},
		{image.DownloadSnapOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, nil},
		{image.DownloadSnapOptions{
			Channel:  "foo",
			Revision: snap.R(42),
		}, nil},
		{image.DownloadSnapOptions{
			Channel:   "foo",
			CohortKey: "bar",
		}, nil},
		{image.DownloadSnapOptions{
			Revision:  snap.R(1),
			CohortKey: "bar",
		}, image.ErrRevisionAndCohort},
		{image.DownloadSnapOptions{
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
	opts := image.DownloadSnapOptions{
		TargetDir: dlDir,
	}
	dlSnap, err := s.tsto.DownloadSnap("core", opts)
	c.Assert(err, check.IsNil)
	c.Check(dlSnap.Path, check.Matches, filepath.Join(dlDir, `core_\d+.snap`))
	c.Check(dlSnap.Info.SnapName(), check.Equals, "core")
	c.Check(dlSnap.RedirectChannel, check.Equals, "")

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

// treeLines is used to sort the output from find
type treeLines []string

func (t treeLines) Len() int {
	return len(t)
}
func (t treeLines) Less(i, j int) bool {
	// strip off the first character of the two strings (assuming the strings
	// are at least 1 character long)
	s1 := t[i]
	if len(s1) > 1 {
		s1 = s1[1:]
	}
	s2 := t[j]
	if len(s2) > 1 {
		s2 = s2[1:]
	}
	return s1 < s2
}
func (t treeLines) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
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
	gadgetInfo, err := gadget.ReadInfoAndValidate(gadgetRoot, model, nil)
	c.Assert(err, check.IsNil)

	err = image.WriteResolvedContent(prepareImageDir, gadgetInfo, gadgetRoot, kernelRoot)
	c.Assert(err, check.IsNil)

	// XXX: add testutil.DirEquals([][]string)
	cmd := exec.Command("find", ".", "-printf", "%y %P\n")
	cmd.Dir = prepareImageDir
	tree, err := cmd.CombinedOutput()
	c.Assert(err, check.IsNil)
	// sort the tree output
	lines := strings.Split(string(tree), "\n")
	sort.Sort(treeLines(lines))
	c.Check(strings.Join(lines, "\n"), check.Equals, `
d 
d resolved-content
d resolved-content/vol1
l resolved-content/vol1/part1
d resolved-content/vol1/part2
d resolved-content/vol1/part2/EFI
d resolved-content/vol1/part2/EFI/boot
f resolved-content/vol1/part2/EFI/boot/grubx64.efi
d resolved-content/vol2
d resolved-content/vol2/part0
d resolved-content/vol2/part0/subdir
f resolved-content/vol2/part0/subdir/foo
d system-seed
d system-seed/EFI
d system-seed/EFI/boot
f system-seed/EFI/boot/system-seed.efi`)

	// check symlink target for "ubuntu-seed" -> <prepareImageDir>/system-seed
	t, err := os.Readlink(filepath.Join(prepareImageDir, "resolved-content/vol1/part1"))
	c.Assert(err, check.IsNil)
	c.Check(t, check.Equals, uc20systemSeed)
}
