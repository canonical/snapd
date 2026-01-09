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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	c.Assert(os.Chmod(tempdir, 0o755), IsNil)

	// use meta/snap.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(snapYamlContent), 0o644)
	c.Assert(err, IsNil)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`

	// an example binary
	binDir := filepath.Join(tempdir, "bin")
	err = os.Mkdir(binDir, 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(binDir, "hello-world"), []byte(helloBinContent), 0o755)
	c.Assert(err, IsNil)

	// unusual permissions for dir
	tmpDir := filepath.Join(tempdir, "tmp")
	err = os.Mkdir(tmpDir, 0o755)
	c.Assert(err, IsNil)
	// avoid umask
	err = os.Chmod(tmpDir, 0o1777)
	c.Assert(err, IsNil)

	// and file
	someFile := filepath.Join(tempdir, "file-with-perm")
	err = os.WriteFile(someFile, []byte(""), 0o666)
	c.Assert(err, IsNil)
	err = os.Chmod(someFile, 0o666)
	c.Assert(err, IsNil)

	// an example symlink
	err = os.Symlink("bin/hello-world", filepath.Join(tempdir, "symlink"))
	c.Assert(err, IsNil)

	return tempdir
}

func makeExampleComponentSourceDir(c *C, componentYaml string) string {
	tempdir := c.MkDir()
	c.Assert(os.Chmod(tempdir, 0o755), IsNil)

	// use meta/snap.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(metaDir, "component.yaml"), []byte(componentYaml), 0o644)
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
	c.Check(err, testutil.ErrorIs, snap.ErrMissingPaths)
	c.Assert(err, ErrorMatches, `snap is unusable due to missing files: path "bin/hello-world" does not exist`)
}

func (s *packSuite) TestPackKernelGadgetOSAppWithConfigureHookHappy(c *C) {
	for _, snapType := range []string{"kernel", "gadget", "os", "app"} {
		snapYaml := fmt.Sprintf(`name: %[1]s
version: 0
type: %[1]s`, snapType)
		sourceDir := makeExampleSnapSourceDir(c, snapYaml)
		c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0o755), IsNil)
		c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", "configure"), []byte("#!/bin/sh"), 0o755), IsNil)
		_, err := pack.Pack(sourceDir, pack.Defaults)
		c.Assert(err, IsNil)
	}
}

func (s *packSuite) TestPackKernelGadgetAppWithDefaultConfigureAndConfigureHookHappy(c *C) {
	for _, snapType := range []string{"kernel", "gadget", "app"} {
		snapYaml := fmt.Sprintf(`name: %[1]s
version: 0
type: %[1]s`, snapType)
		sourceDir := makeExampleSnapSourceDir(c, snapYaml)
		configureHooks := []string{"default-configure", "configure"}
		c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0o755), IsNil)
		for _, hook := range configureHooks {
			c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", hook), []byte("#!/bin/sh"), 0o755), IsNil)
		}
		_, err := pack.Pack(sourceDir, pack.Defaults)
		c.Assert(err, IsNil)
	}
}

func (s *packSuite) TestPackSnapdBaseWithConfigureHookError(c *C) {
	for _, snapType := range []string{"snapd", "base"} {
		snapYaml := fmt.Sprintf(`name: %[1]s
version: 0
type: %[1]s`, snapType)
		sourceDir := makeExampleSnapSourceDir(c, snapYaml)
		c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0o755), IsNil)
		c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", "configure"), []byte("#!/bin/sh"), 0o755), IsNil)
		_, err := pack.Pack(sourceDir, pack.Defaults)
		c.Check(err, ErrorMatches, fmt.Sprintf(`cannot validate snap %[1]q: cannot specify "configure" hook for %[1]q snap %[1]q`, snapType))
	}
}

func (s *packSuite) TestPackSnapdBaseOSWithDefaultConfigureHookError(c *C) {
	for _, snapType := range []string{"snapd", "base", "os"} {
		snapYaml := fmt.Sprintf(`name: %[1]s
version: 0
type: %[1]s`, snapType)
		sourceDir := makeExampleSnapSourceDir(c, snapYaml)
		c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0o755), IsNil)
		c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", "default-configure"), []byte("#!/bin/sh"), 0o755), IsNil)
		_, err := pack.Pack(sourceDir, pack.Defaults)
		// an error due to a prohibited hook for the snap type takes precedence over the
		// error for missing a configure hook when default-configure is present
		c.Check(err, ErrorMatches, fmt.Sprintf(`cannot validate snap %[1]q: cannot specify "default-configure" hook for %[1]q snap %[1]q`, snapType))
	}
}

func (s *packSuite) TestPackDefaultConfigureWithoutConfigureError(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", "default-configure"), []byte("#!/bin/sh"), 0o755), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Check(err, ErrorMatches, `cannot validate snap "hello": cannot specify "default-configure" hook without "configure" hook`)
}

func (s *packSuite) TestPackConfigureHooksPermissionsError(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 0
apps:
 foo:
  command: bin/hello-world
`)
	c.Assert(os.Mkdir(filepath.Join(sourceDir, "meta", "hooks"), 0o755), IsNil)
	configureHooks := []string{"configure", "default-configure"}
	for _, hook := range configureHooks {
		c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "hooks", hook), []byte("#!/bin/sh"), 0o644), IsNil)
		_, err := pack.Pack(sourceDir, pack.Defaults)
		c.Check(err, testutil.ErrorIs, snap.ErrBadModes)
		c.Check(err, ErrorMatches, fmt.Sprintf(`snap is unusable due to bad permissions: "meta/hooks/%s" should be executable, and isn't: -rw-r--r--`, hook))
		// Fix hook error to catch next hook's error
		c.Assert(os.Chmod(filepath.Join(sourceDir, "meta", "hooks", hook), 0o755), IsNil)
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
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "snapshots.yaml"), []byte(invalidSnapshotYaml), 0o444), IsNil)
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
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "snapshots.yaml"), []byte(invalidSnapshotYaml), 0o411), IsNil)
	_, err := pack.Pack(sourceDir, pack.Defaults)
	c.Check(err, testutil.ErrorIs, snap.ErrBadModes)
	c.Assert(err, ErrorMatches, `snap is unusable due to bad permissions: "meta/snapshots.yaml" should be world-readable, and isn't: -r----x--x`)
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
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "meta", "snapshots.yaml"), []byte(invalidSnapshotYaml), 0o444), IsNil)
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
	c.Check(err, testutil.ErrorIs, snap.ErrMissingPaths)
	c.Assert(err, ErrorMatches, `snap is unusable due to missing files: path "bin/hello-world" does not exist`)
	c.Check(buf.String(), Equals, "")
}

func (s *packSuite) TestPackExcludesBackups(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "{name: hello, version: 0}")
	target := c.MkDir()
	// add a backup file
	c.Assert(os.WriteFile(filepath.Join(sourceDir, "foo~"), []byte("hi"), 0o755), IsNil)
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
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "DEBIAN", "foo"), 0o755), IsNil)
	// and a non-toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "bar", "DEBIAN", "baz"), 0o755), IsNil)
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
	c.Assert(os.Mkdir(filepath.Join(sourceDir, ".bzr"), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(sourceDir, ".bzr", "foo"), []byte("hi"), 0o755), IsNil)
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

func (s *packSuite) TestPackComponentSimple(c *C) {
	sourceDir := makeExampleComponentSourceDir(c, `component: hello+test
type: standard
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

func (s *packSuite) TestPackComponentProvenance(c *C) {
	sourceDir := makeExampleComponentSourceDir(c, `component: hello+test
type: standard
version: 1.0.1
provenance: prov
`)

	result, err := pack.Pack(sourceDir, nil)
	c.Assert(err, IsNil)

	// check that there is result
	_, err = os.Stat(result)
	c.Assert(err, IsNil)
	c.Assert(result, Equals, "hello+test_1.0.1.comp")

	// check that the content looks sane
	output, err := exec.Command("unsquashfs", "-ll", result).CombinedOutput()
	c.Assert(err, IsNil)
	expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta("meta/component.yaml"))
	c.Assert(string(output), Matches, expr)
}

func (s *packSuite) TestPackComponentNoVersion(c *C) {
	sourceDir := makeExampleComponentSourceDir(c, `component: hello+test
type: standard
`)

	result, err := pack.Pack(sourceDir, nil)
	c.Assert(err, IsNil)

	// check that there is result
	_, err = os.Stat(result)
	c.Assert(err, IsNil)
	c.Assert(result, Equals, "hello+test.comp")

	// check that the content looks sane
	output, err := exec.Command("unsquashfs", "-ll", result).CombinedOutput()
	c.Assert(err, IsNil)
	expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta("meta/component.yaml"))
	c.Assert(string(output), Matches, expr)
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
	err := os.WriteFile(filepath.Join(sourceDir, "meta/gadget.yaml"), []byte(gadgetYamlContent), 0o644)
	c.Assert(err, IsNil)

	outputDir := filepath.Join(c.MkDir(), "output")
	absSnapFile := filepath.Join(c.MkDir(), "foo.snap")

	// gadget validation fails during layout
	_, err = pack.Pack(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, ErrorMatches, `structure #1 \("bare-struct"\): content "bare.img": stat .*/bare.img: no such file or directory`)

	err = os.WriteFile(filepath.Join(sourceDir, "bare.img"), []byte("foo"), 0o644)
	c.Assert(err, IsNil)

	// gadget validation fails during content presence checks
	_, err = pack.Pack(sourceDir, &pack.Options{
		TargetDir: outputDir,
		SnapName:  absSnapFile,
	})
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("fs-struct"\), content source:foo/: source path does not exist`)

	err = os.Mkdir(filepath.Join(sourceDir, "foo"), 0o644)
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

	for _, comp := range []string{"", "xz", "lzo"} {
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

	for _, comp := range []string{"gzip", "zstd", "silly"} {
		snapfile, err := pack.Pack(sourceDir, &pack.Options{
			TargetDir:   c.MkDir(),
			Compression: comp,
		})
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot use compression %q", comp))
		c.Assert(snapfile, Equals, "")
	}
}
