// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package integrity_test

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type IntegrityTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&IntegrityTestSuite{})

func (s *IntegrityTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *IntegrityTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *IntegrityTestSuite) TestOffsetJSON(c *C) {
	for _, tc := range []struct {
		input          uint64
		expectedOutput string
	}{
		{0, "0000000000000000"},
		{^uint64(0), "ffffffffffffffff"},
	} {
		offset := integrity.Offset(tc.input)
		json, err := json.Marshal(offset)

		//remove quotes
		json = json[1 : len(json)-1]

		c.Assert(err, IsNil)
		c.Check(string(json), Equals, tc.expectedOutput, Commentf("%v", tc))
	}
}

func (s *IntegrityTestSuite) TestAlign(c *C) {
	align := integrity.Align
	blockSize := uint64(integrity.BlockSize)

	for _, tc := range []struct {
		input          uint64
		expectedOutput uint64
	}{
		{0, 0},
		{1, blockSize},
		{blockSize, blockSize},
		{blockSize + 1, 2 * blockSize},
	} {
		ret := align(tc.input)
		c.Check(ret, Equals, tc.expectedOutput, Commentf("%v", tc))
	}
}

func (s *IntegrityTestSuite) TestCreateHeader(c *C) {
	var integrityMetadata integrity.IntegrityMetadata
	magic := integrity.Magic

	header, err := integrity.CreateHeader(&integrityMetadata)
	c.Assert(err, IsNil)

	magicRead := header[0:len(magic)]
	c.Check(magicRead, DeepEquals, magic)

	nullByte := header[len(header)-1:]
	c.Check(nullByte, DeepEquals, []byte{0x0})
	c.Check(uint64(len(header)), Equals, integrity.Align(uint64(len(header))))
}

func (s *IntegrityTestSuite) TestGenerateAndAppendSuccess(c *C) {
	blockSize := uint64(integrity.BlockSize)
	magic := integrity.Magic
	snapFileName := "foo.snap"

	buildDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(buildDir, "data.bin"), []byte("data"), 0644)
	c.Assert(err, IsNil)

	snapPath := filepath.Join(buildDir, snapFileName)
	snap := squashfs.New(snapPath)
	err = snap.Build(buildDir, &squashfs.BuildOpts{SnapType: "app"})
	c.Assert(err, IsNil)

	snapFileInfo, err := os.Stat(snapPath)
	orig_size := snapFileInfo.Size()

	err = integrity.GenerateAndAppend(snapPath)
	c.Assert(err, IsNil)

	snapFile, err := os.Open(snapPath)
	c.Assert(err, IsNil)
	defer snapFile.Close()

	_, err = snapFile.Seek(orig_size, io.SeekStart)
	c.Assert(err, IsNil)

	// check integrity header
	header := make([]byte, blockSize-1)
	n, err := snapFile.Read(header)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(blockSize)-1)

	metadata := header[len(magic):bytes.IndexByte(header, 0)]
	var integrityMetadata integrity.IntegrityMetadata
	err = json.Unmarshal(metadata, &integrityMetadata)
	c.Check(err, IsNil)
}
