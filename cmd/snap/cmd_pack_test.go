package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/logger"
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
	err = ioutil.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(snapYaml), 0644)
	c.Assert(err, check.IsNil)

	return tempdir
}

func (s *SnapSuite) TestPackCheckSkeletonNoAppFiles(c *check.C) {
	_, r := logger.MockLogger()
	defer r()

	snapDir := makeSnapDirForPack(c, packSnapYaml)

	// check-skeleton does not fail due to missing files
	_, err := snaprun.Parser().ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestPackCheckSkeletonBadMeta(c *check.C) {
	// no snap name
	snapYaml := `
version: foobar
apps:
`
	snapDir := makeSnapDirForPack(c, snapYaml)

	_, err := snaprun.Parser().ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.ErrorMatches, "snap name cannot be empty")
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

	_, err := snaprun.Parser().ParseArgs([]string{"pack", "--check-skeleton", snapDir})
	c.Assert(err, check.ErrorMatches, `application ("bar" common-id "org.foo.foo" must be unique, already used by application "foo"|"foo" common-id "org.foo.foo" must be unique, already used by application "bar")`)
}

func (s *SnapSuite) TestPackPacksFailsForMissingPaths(c *check.C) {
	_, r := logger.MockLogger()
	defer r()

	snapDir := makeSnapDirForPack(c, packSnapYaml)

	_, err := snaprun.Parser().ParseArgs([]string{"pack", snapDir, snapDir})
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
	err = ioutil.WriteFile(filepath.Join(binDir, "hello"), []byte(helloBinContent), 0755)
	c.Assert(err, check.IsNil)

	_, err = snaprun.Parser().ParseArgs([]string{"pack", snapDir, snapDir})
	c.Assert(err, check.IsNil)

	matches, err := filepath.Glob(snapDir + "/hello*.snap")
	c.Assert(err, check.IsNil)
	c.Assert(matches, check.HasLen, 1)
}
