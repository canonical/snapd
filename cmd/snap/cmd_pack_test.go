package main_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/check.v1"

	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

const packSnapYaml = `name: hello
version: 1.0.1
apps:
 app:
  command: bin/hello
`

func makeSnapDirForPack(c *check.C, snapYaml string) string {
	tempdir := c.MkDir()
	c.Assert(os.Chmod(tempdir, 0755), check.IsNil)

	// use meta/snap.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(snapYaml), 0644)
	c.Assert(err, check.IsNil)

	return tempdir
}

func makeComponentDirForPack(c *check.C, compYaml string) string {
	tempdir := c.MkDir()
	c.Assert(os.Chmod(tempdir, 0755), check.IsNil)

	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(filepath.Join(metaDir, "component.yaml"), []byte(compYaml), 0644)
	c.Assert(err, check.IsNil)

	return tempdir
}

func (s *SnapSuite) TestPackCheckSkeletonNoAppFiles(c *check.C) {
	_, r := logger.MockLogger()
	defer r()

	snapDir := makeSnapDirForPack(c, packSnapYaml)

	// check-skeleton does not fail due to missing files
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestPackCheckSkeletonBadMeta(c *check.C) {
	// no snap name
	snapYaml := `
version: foobar
apps:
`
	snapDir := makeSnapDirForPack(c, snapYaml)

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.ErrorMatches, `cannot validate snap "": snap name cannot be empty`)
}

func (s *SnapSuite) TestPackCheckSkeletonConflictingCommonID(c *check.C) {
	// conflicting common-id
	snapYaml := `name: foo
version: foobar
apps:
  foo:
    common-id: org.foo.foo
  bar:
    common-id: org.foo.foo
`
	snapDir := makeSnapDirForPack(c, snapYaml)

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.ErrorMatches, `cannot validate snap "foo": application ("bar" common-id "org.foo.foo" must be unique, already used by application "foo"|"foo" common-id "org.foo.foo" must be unique, already used by application "bar")`)
}

func (s *SnapSuite) TestPackCheckSkeletonWonkyInterfaces(c *check.C) {
	snapYaml := `
name: foo
version: 1.0.1
slots:
  kale:
`
	snapDir := makeSnapDirForPack(c, snapYaml)

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.IsNil)
	c.Check(s.stderr.String(), check.Equals, "snap \"foo\" has bad plugs or slots: kale (unknown interface \"kale\")\n")
}

func (s *SnapSuite) TestPackPacksFailsForMissingPaths(c *check.C) {
	_, r := logger.MockLogger()
	defer r()

	snapDir := makeSnapDirForPack(c, packSnapYaml)

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", snapDir, snapDir})
	c.Assert(err, check.ErrorMatches, ".* snap is unusable due to missing files")
}

func (s *SnapSuite) TestPackPacksASnap(c *check.C) {
	snapDir := makeSnapDirForPack(c, packSnapYaml)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`
	// an example binary
	binDir := filepath.Join(snapDir, "bin")
	err := os.Mkdir(binDir, 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(filepath.Join(binDir, "hello"), []byte(helloBinContent), 0755)
	c.Assert(err, check.IsNil)

	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", snapDir, snapDir})
	c.Assert(err, check.IsNil)

	matches, err := filepath.Glob(snapDir + "/hello*.snap")
	c.Assert(err, check.IsNil)
	c.Assert(matches, check.HasLen, 1)
}

func (s *SnapSuite) TestPackPacksASnapWithCompressionHappy(c *check.C) {
	snapDir := makeSnapDirForPack(c, "name: hello\nversion: 1.0")

	for _, comp := range []string{"xz", "lzo", "zstd"} {
		_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--compression", comp, snapDir, snapDir})
		c.Assert(err, check.IsNil)

		matches, err := filepath.Glob(snapDir + "/hello*.snap")
		c.Assert(err, check.IsNil)
		c.Assert(matches, check.HasLen, 1)
		err = os.Remove(matches[0])
		c.Assert(err, check.IsNil)
	}
}

func (s *SnapSuite) TestPackPacksASnapWithCompressionUnhappy(c *check.C) {
	snapDir := makeSnapDirForPack(c, "name: hello\nversion: 1.0")

	for _, comp := range []string{"gzip", "silly"} {
		_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--compression", comp, snapDir, snapDir})
		c.Assert(err, check.ErrorMatches, fmt.Sprintf(`cannot pack "/.*": cannot use compression %q`, comp))
	}
}

func (s *SnapSuite) TestPackPacksASnapWithIntegrityHappy(c *check.C) {
	snapDir := makeSnapDirForPack(c, "name: hello\nversion: 1.0")

	// mock the verity-setup command, what it does is make a copy of the snap
	// and then returns pre-calculated output
	vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`
case "$1" in
	--version)
		echo "veritysetup 2.2.6"
		exit 0
		;;
	format)
		cp %[1]s/hello_1.0_all.snap %[1]s/hello_1.0_all.snap.verity
		echo "VERITY header information for %[1]s/hello_1.0_all.snap.verity"
		echo "UUID:            	8f6dcdd2-9426-49d8-9879-a5c87fc78c15"
		echo "Hash type:       	1"
		echo "Data blocks:     	1"
		echo "Data block size: 	4096"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	06d01a87b298b6855b6a3a1b32450deba4550417cbec2bb21a38d6dda24a1b53"
		echo "Root hash:      	306398e250a950ea1cbfceda608ee4585f053323251b08b7ed3f004740e91ba5"
		;;
esac
`, snapDir))
	defer vscmd.Restore()

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", "--append-integrity-data", snapDir, snapDir})
	c.Assert(err, check.IsNil)

	snapOriginal := path.Join(snapDir, "hello_1.0_all.snap")
	snapVerity := snapOriginal + ".verity"
	c.Assert(vscmd.Calls(), check.HasLen, 2)
	c.Check(vscmd.Calls()[0], check.DeepEquals, []string{"veritysetup", "--version"})
	c.Check(vscmd.Calls()[1], check.DeepEquals, []string{"veritysetup", "format", snapOriginal, snapVerity})

	matches, err := filepath.Glob(snapDir + "/hello*.snap")
	c.Assert(err, check.IsNil)
	c.Assert(matches, check.HasLen, 1)
	err = os.Remove(matches[0])
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestPackComponentHappy(c *check.C) {
	const compYaml = `component: snap+comp
version: 12a
type: test
`
	_, r := logger.MockLogger()
	defer r()

	snapDir := makeComponentDirForPack(c, compYaml)

	// check-skeleton does not fail due to missing files
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", snapDir})
	c.Assert(err, check.IsNil)
	err = os.Remove("snap+comp_12a.comp")
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestPackComponentBadName(c *check.C) {
	const compYaml = `component: snapcomp
version: 12a
type: test
`
	_, r := logger.MockLogger()
	defer r()

	snapDir := makeComponentDirForPack(c, compYaml)

	// check-skeleton does not fail due to missing files
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"pack", snapDir})
	c.Assert(err, check.ErrorMatches, `.*: cannot parse component.yaml: incorrect component name "snapcomp"`)
}
