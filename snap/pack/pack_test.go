// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package pack_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"

	// for SanitizePlugsSlots
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/pack"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type packSuite struct {
	testutil.BaseTest
}

var _ = Suite(&packSuite{})

func (s *packSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	// chdir into a tempdir
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	s.AddCleanup(func() { os.Chdir(pwd) })
	err = os.Chdir(c.MkDir())
	c.Assert(err, IsNil)

	// use fake root
	dirs.SetRootDir(c.MkDir())
}

func (s *packSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func makeExampleSnapSourceDir(c *C, snapYamlContent string) string {
	tempdir := c.MkDir()
	c.Assert(os.Chmod(tempdir, 0755), IsNil)

	// use meta/snap.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(snapYamlContent), 0644)
	c.Assert(err, IsNil)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`

	// an example binary
	binDir := filepath.Join(tempdir, "bin")
	err = os.Mkdir(binDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(binDir, "hello-world"), []byte(helloBinContent), 0755)
	c.Assert(err, IsNil)

	// unusual permissions for dir
	tmpDir := filepath.Join(tempdir, "tmp")
	err = os.Mkdir(tmpDir, 0755)
	c.Assert(err, IsNil)
	// avoid umask
	err = os.Chmod(tmpDir, 01777)
	c.Assert(err, IsNil)

	// and file
	someFile := filepath.Join(tempdir, "file-with-perm")
	err = ioutil.WriteFile(someFile, []byte(""), 0666)
	c.Assert(err, IsNil)
	err = os.Chmod(someFile, 0666)
	c.Assert(err, IsNil)

	// an example symlink
	err = os.Symlink("bin/hello-world", filepath.Join(tempdir, "symlink"))
	c.Assert(err, IsNil)

	return tempdir
}

func (s *packSuite) TestPackNoManifestFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	c.Assert(os.Remove(filepath.Join(sourceDir, "meta", "snap.yaml")), IsNil)
	_, err := pack.Snap(sourceDir, pack.Defaults)
	c.Assert(err, ErrorMatches, `.*/meta/snap\.yaml: no such file or directory`)
}

func (s *packSuite) TestPackMissingAppFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Remove(filepath.Join(sourceDir, "bin", "hello-world")), IsNil)
	_, err := pack.Snap(sourceDir, pack.Defaults)
	c.Assert(err, Equals, snap.ErrMissingPaths)
}

func (s *packSuite) TestValidateMissingAppFailsWithErrMissingPaths(c *C) {
	var buf bytes.Buffer
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
  plugs: [potato]
`)
	err := pack.CheckSkeleton(&buf, sourceDir)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "snap \"hello\" has bad plugs or slots: potato (unknown interface \"potato\")\n")

	buf.Reset()
	c.Assert(os.Remove(filepath.Join(sourceDir, "bin", "hello-world")), IsNil)

	err = pack.CheckSkeleton(&buf, sourceDir)
	c.Assert(err, Equals, snap.ErrMissingPaths)
	c.Check(buf.String(), Equals, "")
}

func (s *packSuite) TestPackExcludesBackups(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	target := c.MkDir()
	// add a backup file
	c.Assert(ioutil.WriteFile(filepath.Join(sourceDir, "foo~"), []byte("hi"), 0755), IsNil)
	snapfile, err := pack.Snap(sourceDir, &pack.Options{TargetDir: c.MkDir()})
	c.Assert(err, IsNil)
	c.Assert(squashfs.New(snapfile).Unpack("*", target), IsNil)

	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: foo~`)
}

func (s *packSuite) TestPackExcludesTopLevelDEBIAN(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	target := c.MkDir()
	// add a toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "DEBIAN", "foo"), 0755), IsNil)
	// and a non-toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "bar", "DEBIAN", "baz"), 0755), IsNil)
	snapfile, err := pack.Snap(sourceDir, &pack.Options{TargetDir: c.MkDir()})
	c.Assert(err, IsNil)
	c.Assert(squashfs.New(snapfile).Unpack("*", target), IsNil)
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: DEBIAN`)
	// but *only one* DEBIAN is skipped
	c.Check(strings.Count(string(out), "Only in"), Equals, 1)
}

func (s *packSuite) TestPackExcludesWholeDirs(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	target := c.MkDir()
	// add a file inside a skipped dir
	c.Assert(os.Mkdir(filepath.Join(sourceDir, ".bzr"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(sourceDir, ".bzr", "foo"), []byte("hi"), 0755), IsNil)
	snapfile, err := pack.Snap(sourceDir, &pack.Options{TargetDir: c.MkDir()})
	c.Assert(err, IsNil)
	c.Assert(squashfs.New(snapfile).Unpack("*", target), IsNil)
	out, _ := exec.Command("find", sourceDir).Output()
	c.Check(string(out), Not(Equals), "")
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err = cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: \.bzr`)
}

func (s *packSuite) TestDebArchitecture(c *C) {
	c.Check(pack.DebArchitecture(&snap.Info{Architectures: []string{"foo"}}), Equals, "foo")
	c.Check(pack.DebArchitecture(&snap.Info{Architectures: []string{"foo", "bar"}}), Equals, "multi")
	c.Check(pack.DebArchitecture(&snap.Info{Architectures: nil}), Equals, "all")
}

func (s *packSuite) TestPackSimple(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
architectures: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	outputDir := filepath.Join(c.MkDir(), "output")
	absSnapFile := filepath.Join(c.MkDir(), "foo.snap")

	type T struct {
		outputDir, filename, expected string
	}

	table := []T{
		// no output dir, no filename -> default in .
		{"", "", "hello_1.0.1_multi.snap"},
		// no output dir, relative filename -> filename in .
		{"", "foo.snap", "foo.snap"},
		// no putput dir, absolute filename -> absolute filename
		{"", absSnapFile, absSnapFile},
		// output dir, no filename -> default in outputdir
		{outputDir, "", filepath.Join(outputDir, "hello_1.0.1_multi.snap")},
		// output dir, relative filename -> filename in outputDir
		{filepath.Join(outputDir, "inner"), "../foo.snap", filepath.Join(outputDir, "foo.snap")},
		// output dir, absolute filename -> absolute filename
		{outputDir, absSnapFile, absSnapFile},
	}

	for i, t := range table {
		comm := Commentf("%d", i)
		resultSnap, err := pack.Snap(sourceDir, &pack.Options{
			TargetDir: t.outputDir,
			SnapName:  t.filename,
		})
		c.Assert(err, IsNil, comm)

		// check that there is result
		_, err = os.Stat(resultSnap)
		c.Assert(err, IsNil, comm)
		c.Assert(resultSnap, Equals, t.expected, comm)

		// check that the content looks sane
		output, err := exec.Command("unsquashfs", "-ll", resultSnap).CombinedOutput()
		c.Assert(err, IsNil, comm)
		for _, needle := range []string{
			"meta/snap.yaml",
			"bin/hello-world",
			"symlink -> bin/hello-world",
		} {
			expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta(needle))
			c.Assert(string(output), Matches, expr, comm)
		}
	}

}

func (s *packSuite) TestPackGadgetValidate(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: funky-gadget
version: 1.0.1
type: gadget
`)

	var gadgetYamlContent = `
volumes:
  bad:
    bootloader: grub
    structure:
      - name: fs-struct
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
        content:
          - source: foo/
            target: /
      - name: bare-struct
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        content:
          - image: bare.img

`
	err := ioutil.WriteFile(filepath.Join(sourceDir, "meta/gadget.yaml"), []byte(gadgetYamlContent), 0644)
	c.Assert(err, IsNil)

	outputDir := filepath.Join(c.MkDir(), "output")
	absSnapFile := filepath.Join(c.MkDir(), "foo.snap")

	// gadget validation fails during layout
	_, err = pack.Snap(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, ErrorMatches, `invalid layout of volume "bad": cannot lay out structure #1 \("bare-struct"\): content "bare.img": stat .*/bare.img: no such file or directory`)

	err = ioutil.WriteFile(filepath.Join(sourceDir, "bare.img"), []byte("foo"), 0644)
	c.Assert(err, IsNil)

	// gadget validation fails during content presence checks
	_, err = pack.Snap(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("fs-struct"\), content source:foo/: source path does not exist`)

	err = os.Mkdir(filepath.Join(sourceDir, "foo"), 0644)
	c.Assert(err, IsNil)
	// all good now
	_, err = pack.Snap(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, IsNil)
}

func (s *packSuite) TestPackWithCompressionHappy(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")

	for _, comp := range []string{"", "xz", "lzo"} {
		snapfile, err := pack.Snap(sourceDir, &pack.Options{
			TargetDir:   c.MkDir(),
			Compression: comp,
		})
		c.Assert(err, IsNil)
		c.Assert(snapfile, testutil.FilePresent)
	}
}

func (s *packSuite) TestPackWithCompressionUnhappy(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")

	for _, comp := range []string{"gzip", "zstd", "silly"} {
		snapfile, err := pack.Snap(sourceDir, &pack.Options{
			TargetDir:   c.MkDir(),
			Compression: comp,
		})
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot use compression %q", comp))
		c.Assert(snapfile, Equals, "")
	}
}

func (s *packSuite) TestPackWithIntegrity(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	targetDir := c.MkDir()

	// mock the verity-setup command, what it does is make a copy of the snap
	// and then returns pre-calculated output
	vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`
case "$1" in
	--version)
		echo "veritysetup 2.2.6"
		exit 0
		;;
	format)
		cp %[1]s/hello_0_all.snap %[1]s/hello_0_all.snap.verity
		echo "VERITY header information for %[1]s/hello_0_all.snap.verity"
		echo "UUID:            	606d10a2-24d8-4c6b-90cf-68207aa7c850"
		echo "Hash type:       	1"
		echo "Data blocks:     	1"
		echo "Data block size: 	4096"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	eba61f2091bb6122226aef83b0d6c1623f095fc1fda5712d652a8b34a02024ea"
		echo "Root hash:      	3fbfef5f1f0214d727d03eebc4723b8ef5a34740fd8f1359783cff1ef9c3f334"
		;;
esac
`, targetDir))
	defer vscmd.Restore()

	snapPath, err := pack.Snap(sourceDir, &pack.Options{
		TargetDir: targetDir,
		Integrity: true,
	})
	c.Assert(err, IsNil)
	c.Check(snapPath, testutil.FilePresent)
	c.Assert(vscmd.Calls(), HasLen, 2)
	c.Check(vscmd.Calls()[0], DeepEquals, []string{"veritysetup", "--version"})
	c.Check(vscmd.Calls()[1], DeepEquals, []string{"veritysetup", "format", snapPath, snapPath + ".verity"})

	magic := []byte{'s', 'n', 'a', 'p', 'e', 'x', 't'}

	snapFile, err := os.Open(snapPath)
	c.Assert(err, IsNil)
	defer snapFile.Close()

	// example snap has a size of 4096 (1 block)
	_, err = snapFile.Seek(4096, io.SeekStart)
	c.Assert(err, IsNil)

	integrityHdr := make([]byte, 4096)
	_, err = snapFile.Read(integrityHdr)
	c.Assert(err, IsNil)

	c.Check(bytes.HasPrefix(integrityHdr, magic), Equals, true)

	var hdr interface{}
	integrityHdr = bytes.Trim(integrityHdr, "\x00")
	err = json.Unmarshal(integrityHdr[len(magic):], &hdr)
	c.Check(err, IsNil)

	integrityDataHeader, ok := hdr.(map[string]interface{})
	c.Assert(ok, Equals, true)
	hdrSizeStr, ok := integrityDataHeader["size"].(string)
	c.Assert(ok, Equals, true)
	hdrSize, err := strconv.ParseUint(hdrSizeStr, 10, 64)
	c.Assert(err, IsNil)
	c.Check(hdrSize, Equals, uint64(2*4096))

	fi, err := snapFile.Stat()
	c.Assert(err, IsNil)
	c.Check(fi.Size(), Equals, int64(3*4096))
}
