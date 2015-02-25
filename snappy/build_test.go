package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

func makeExampleSnapSourceDir(c *C) string {
	tempdir := c.MkDir()

	const packageYaml = `
name: foo
version: 1.0
vendor: Foo <foo@example.com>
`

	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "package.yaml"), []byte(packageYaml), 0644)
	c.Assert(err, IsNil)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`

	binDir := filepath.Join(tempdir, "bin")
	err = os.Mkdir(binDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(binDir, "hello-world"), []byte(helloBinContent), 0755)
	c.Assert(err, IsNil)

	return tempdir
}

func (s *SnapTestSuite) TestBuildSimple(c *C) {
	sourceDir := makeExampleSnapSourceDir(c)

	resultSnap, err := Build(sourceDir)
	c.Assert(err, IsNil)

	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
}
