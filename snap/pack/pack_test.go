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
	"github.com/snapcore/snapd/snap/integrity"
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
	err = os.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(snapYamlContent), 0644)
	c.Assert(err, IsNil)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`

	// an example binary
	binDir := filepath.Join(tempdir, "bin")
	err = os.Mkdir(binDir, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(binDir, "hello-world"), []byte(helloBinContent), 0755)
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
	err = os.WriteFile(someFile, []byte(""), 0666)
	c.Assert(err, IsNil)
	err = os.Chmod(someFile, 0666)
	c.Assert(err, IsNil)

	// an example symlink
	err = os.Symlink("bin/hello-world", filepath.Join(tempdir, "symlink"))
	c.Assert(err, IsNil)

	return tempdir
}

func makeExampleComponentSourceDir(c *C, componentYaml string) string {
	tempdir := c.MkDir()
	c.Assert(os.Chmod(tempdir, 0755), IsNil)

	// use meta/snap.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(metaDir, "component.yaml"), []byte(componentYaml), 0644)
	c.Assert(err, IsNil)
	return tempdir
}

func (s *packSuite) TestPackNoManifestFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	c.Assert(os.Remove(filepath.Join(sourceDir, "meta", "snap.yaml")), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(err, ErrorMatches, `.*/meta/snap\.yaml: no such file or directory`)
}

func (s *packSuite) TestPackInfoFromSnapYamlFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
no-colon
`)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(err, ErrorMatches, `cannot parse snap.yaml: yaml: line 4: could not find expected ':'`)
}

func (s *packSuite) TestPackComponentBadName(c *C) {
	sourceDir := makeExampleComponentSourceDir(c, "{component: hello, version: 0}")
	pathName, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(pathName, Equals, "")
	c.Assert(err.Error(), Equals, `cannot parse component.yaml: incorrect component name "hello"`)
}

func (s *packSuite) TestPackComponentBadYaml(c *C) {
	sourceDir := makeExampleComponentSourceDir(c, "...")
	pathName, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(pathName, Equals, "")
	c.Assert(err.Error(), Equals, `cannot parse component.yaml: yaml: did not find expected node content`)
}

func (s *packSuite) TestPackMissingAppFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Remove(filepath.Join(sourceDir, "bin", "hello-world")), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(err, Equals, snap.ErrMissingPaths)
}

func (s *packSuite) TestPackDefaultConfigureWithoutConfigureError(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", "default-configure"), []byte("#!/bin/sh"), 0755), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Check(err, ErrorMatches, "cannot validate snap \"hello\": cannot specify \"default-configure\" hook without \"configure\" hook")
}

func (s *packSuite) TestPackConfigureHooksPermissionsError(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0755), IsNil)
	configureHooks := []string{"configure", "default-configure"}
	for _, hook := range configureHooks {
		c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", hook), []byte("#!/bin/sh"), 0666), IsNil)
		_, err := pack.Pack(sourceDir, pack.Defaults)
		c.Check(err, ErrorMatches, "snap is unusable due to bad permissions")
	}
}

func (s *packSuite) TestPackConfigureHooksHappy(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0755), IsNil)
	configureHooks := []string{"configure", "default-configure"}
	for _, hook := range configureHooks {
		c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", hook), []byte("#!/bin/sh"), 0755), IsNil)
		_, err := pack.Pack(sourceDir, pack.Defaults)
		c.Assert(err, IsNil)
	}
}

func (s *packSuite) TestPackSnapshotYamlExcludePathError(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)

	invalidSnapshotYaml := `exclude:
    - $SNAP_DATA/one
    - $SNAP_UNKNOWN_DIR/two
`
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "snapshots.yaml"), []byte(invalidSnapshotYaml), 0444), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(err, ErrorMatches, "snapshot exclude path must start with one of.*")
}

func (s *packSuite) TestPackSnapshotYamlPermissionsError(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)

	invalidSnapshotYaml := `exclude:
    - $SNAP_DATA/one
    - $SNAP_COMMON/two
`
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "snapshots.yaml"), []byte(invalidSnapshotYaml), 0411), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(err, ErrorMatches, "snap is unusable due to bad permissions")
}

func (s *packSuite) TestPackSnapshotYamlHappy(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)

	invalidSnapshotYaml := `exclude:
    - $SNAP_DATA/one
    - $SNAP_COMMON/two
`
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "snapshots.yaml"), []byte(invalidSnapshotYaml), 0444), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Assert(err, IsNil)
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
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "foo~"), []byte("hi"), 0755), IsNil)
	snapfile, err := pack.Pack(sourceDir, &pack.Options{TargetDir: c.MkDir()})
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
	snapfile, err := pack.Pack(sourceDir, &pack.Options{TargetDir: c.MkDir()})
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
	c.Assert(os.WriteFile(filepath.Join(sourceDir, ".bzr", "foo"), []byte("hi"), 0755), IsNil)
	snapfile, err := pack.Pack(sourceDir, &pack.Options{TargetDir: c.MkDir()})
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
	sourceDir := makeExampleComponentSourceDir(c, `component: hello+test
type: test
version: 1.0.1
`)

	outputDir := filepath.Join(c.MkDir(), "output")
	absSnapFile := filepath.Join(c.MkDir(), "foo.comp")

	type T struct {
		outputDir, filename, expected string
	}

	table := []T{
		// no output dir, no filename -> default in .
		{"", "", "hello+test_1.0.1.comp"},
		// no output dir, relative filename -> filename in .
		{"", "foo.comp", "foo.comp"},
		// no putput dir, absolute filename -> absolute filename
		{"", absSnapFile, absSnapFile},
		// output dir, no filename -> default in outputdir
		{outputDir, "", filepath.Join(outputDir, "hello+test_1.0.1.comp")},
		// output dir, relative filename -> filename in outputDir
		{filepath.Join(outputDir, "inner"), "../foo.comp", filepath.Join(outputDir, "foo.comp")},
		// output dir, absolute filename -> absolute filename
		{outputDir, absSnapFile, absSnapFile},
	}

	for i, t := range table {
		comm := Commentf("%d", i)
		resultSnap, err := pack.Pack(sourceDir, &pack.Options{
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
		expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta("meta/component.yaml"))
		c.Assert(string(output), Matches, expr, comm)
	}
}

func (s *packSuite) TestPackComponentSimple(c *C) {
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
		resultSnap, err := pack.Pack(sourceDir, &pack.Options{
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
	err := os.WriteFile(filepath.Join(sourceDir, "meta/gadget.yaml"), []byte(gadgetYamlContent), 0644)
	c.Assert(err, IsNil)

	outputDir := filepath.Join(c.MkDir(), "output")
	absSnapFile := filepath.Join(c.MkDir(), "foo.snap")

	// gadget validation fails during layout
	_, err = pack.Pack(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, ErrorMatches, `structure #1 \("bare-struct"\): content "bare.img": stat .*/bare.img: no such file or directory`)

	err = os.WriteFile(filepath.Join(sourceDir, "bare.img"), []byte("foo"), 0644)
	c.Assert(err, IsNil)

	// gadget validation fails during content presence checks
	_, err = pack.Pack(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("fs-struct"\), content source:foo/: source path does not exist`)

	err = os.Mkdir(filepath.Join(sourceDir, "foo"), 0644)
	c.Assert(err, IsNil)
	// all good now
	_, err = pack.Pack(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, IsNil)
}

func (s *packSuite) TestPackWithCompressionHappy(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")

	for _, comp := range []string{"", "xz", "lzo", "zstd"} {
		snapfile, err := pack.Pack(sourceDir, &pack.Options{
			TargetDir:   c.MkDir(),
			Compression: comp,
		})
		c.Assert(err, IsNil)
		c.Assert(snapfile, testutil.FilePresent)
	}
}

func (s *packSuite) TestPackWithCompressionUnhappy(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")

	for _, comp := range []string{"gzip", "silly"} {
		snapfile, err := pack.Pack(sourceDir, &pack.Options{
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

	// 8192 is the hash size that is created when running 'veritysetup format'
	// on a minimally sized snap. there is not an easy way to calculate this
	// value dynamically.
	const verityHashSize = 8192

	// mock the verity-setup command, what it does is make a copy of the snap
	// and then returns pre-calculated output
	vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`
case "$1" in
	--version)
		echo "veritysetup 2.2.6"
		exit 0
		;;
	format)
		truncate -s %[1]d %[2]s/hello_0_all.snap.verity
		echo "VERITY header information for %[2]s/hello_0_all.snap.verity"
		echo "UUID:            	606d10a2-24d8-4c6b-90cf-68207aa7c850"
		echo "Hash type:       	1"
		echo "Data blocks:     	4"
		echo "Data block size: 	4096"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	eba61f2091bb6122226aef83b0d6c1623f095fc1fda5712d652a8b34a02024ea"
		echo "Root hash:      	3fbfef5f1f0214d727d03eebc4723b8ef5a34740fd8f1359783cff1ef9c3f334"
		;;
esac
`, verityHashSize, targetDir))
	defer vscmd.Restore()

	snapPath, err := pack.Pack(sourceDir, &pack.Options{
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

	fi, err := snapFile.Stat()
	c.Assert(err, IsNil)

	integrityStartOffset := squashfs.MinimumSnapSize
	if fi.Size() > int64(65536) {
		// on openSUSE, the squashfs image is padded up to 64k,
		// including the integrator data, the overall size is > 64k
		integrityStartOffset = 65536
	}

	// example snap has a size of 16384 (4 blocks)
	_, err = snapFile.Seek(integrityStartOffset, io.SeekStart)
	c.Assert(err, IsNil)

	integrityHdr := make([]byte, integrity.HeaderSize)
	_, err = snapFile.Read(integrityHdr)
	c.Assert(err, IsNil)

	c.Assert(bytes.HasPrefix(integrityHdr, magic), Equals, true)

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
	c.Check(hdrSize, Equals, uint64(integrity.HeaderSize+verityHashSize))

	fi, err = snapFile.Stat()
	c.Assert(err, IsNil)
	c.Check(fi.Size(), Equals, int64(integrityStartOffset+(integrity.HeaderSize+verityHashSize)))
}
