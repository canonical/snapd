// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	generate "github.com/snapcore/snapd/bootloader/assets/genasset"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type generateAssetsTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&generateAssetsTestSuite{})

func (s *generateAssetsTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func mockArgs(args []string) (restore func()) {
	old := os.Args
	os.Args = args
	return func() {
		os.Args = old
	}
}

func (s *generateAssetsTestSuite) TestArgs(c *C) {
	generate.ResetArgs()
	restore := mockArgs([]string{"self", "-in", "ok", "-out", "ok", "-name", "assetname"})
	defer restore()
	c.Assert(generate.ParseArgs(), IsNil)
	// no input file
	generate.ResetArgs()
	restore = mockArgs([]string{"self", "-out", "ok", "-name", "assetname"})
	defer restore()
	c.Assert(generate.ParseArgs(), ErrorMatches, "input file not provided")
	// no output file
	restore = mockArgs([]string{"self", "-in", "in", "-name", "assetname"})
	defer restore()
	generate.ResetArgs()
	c.Assert(generate.ParseArgs(), ErrorMatches, "output file not provided")
	// no name
	generate.ResetArgs()
	restore = mockArgs([]string{"self", "-in", "in", "-out", "out"})
	defer restore()
	c.Assert(generate.ParseArgs(), ErrorMatches, "asset name not provided")
}

func (s *generateAssetsTestSuite) TestSimpleAsset(c *C) {
	d := c.MkDir()
	err := os.WriteFile(filepath.Join(d, "in"), []byte("this is a\n"+
		"multiline asset \"'``\nwith chars\n"), 0644)
	c.Assert(err, IsNil)
	err = generate.Run("asset-name", filepath.Join(d, "in"), filepath.Join(d, "out"))
	c.Assert(err, IsNil)
	data, err := ioutil.ReadFile(filepath.Join(d, "out"))
	c.Assert(err, IsNil)

	const exp = `// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package assets

// Code generated from %s DO NOT EDIT

func init() {
	registerInternal("asset-name", []byte{
		0x74, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x61, 0x0a, 0x6d, 0x75, 0x6c, 0x74, 0x69, 0x6c,
		0x69, 0x6e, 0x65, 0x20, 0x61, 0x73, 0x73, 0x65, 0x74, 0x20, 0x22, 0x27, 0x60, 0x60, 0x0a, 0x77,
		0x69, 0x74, 0x68, 0x20, 0x63, 0x68, 0x61, 0x72, 0x73, 0x0a,
	})
}
`
	c.Check(string(data), Equals, fmt.Sprintf(exp, filepath.Join(d, "in")))
}

func (s *generateAssetsTestSuite) TestGoFmtClean(c *C) {
	_, err := exec.LookPath("gofmt")
	if err != nil {
		c.Skip("gofmt is missing")
	}

	d := c.MkDir()
	err = os.WriteFile(filepath.Join(d, "in"), []byte("this is a\n"+
		"multiline asset \"'``\nuneven chars\n"), 0644)
	c.Assert(err, IsNil)
	err = generate.Run("asset-name", filepath.Join(d, "in"), filepath.Join(d, "out"))
	c.Assert(err, IsNil)

	cmd := exec.Command("gofmt", "-l", "-d", filepath.Join(d, "out"))
	out, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(out, HasLen, 0, Commentf("output file is not gofmt clean: %s", string(out)))
}

func (s *generateAssetsTestSuite) TestRunErrors(c *C) {
	d := c.MkDir()
	err := generate.Run("asset-name", filepath.Join(d, "missing"), filepath.Join(d, "out"))
	c.Assert(err, ErrorMatches, "cannot open input file: open .*/missing: no such file or directory")

	err = os.WriteFile(filepath.Join(d, "in"), []byte("this is a\n"+
		"multiline asset \"'``\nuneven chars\n"), 0644)
	c.Assert(err, IsNil)

	err = generate.Run("asset-name", filepath.Join(d, "in"), filepath.Join(d, "does-not-exist", "out"))
	c.Assert(err, ErrorMatches, `cannot open output file: open .*/does-not-exist/out\..*: no such file or directory`)

}

func (s *generateAssetsTestSuite) TestFormatLines(c *C) {
	out := generate.FormatLines(bytes.Repeat([]byte{1}, 12))
	c.Check(out, DeepEquals, []string{
		"0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,",
	})
	out = generate.FormatLines(bytes.Repeat([]byte{1}, 16))
	c.Check(out, DeepEquals, []string{
		"0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,",
	})
	out = generate.FormatLines(bytes.Repeat([]byte{1}, 17))
	c.Check(out, DeepEquals, []string{
		"0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,",
		"0x01,",
	})
	out = generate.FormatLines(bytes.Repeat([]byte{1}, 1))
	c.Check(out, DeepEquals, []string{
		"0x01,",
	})
}
