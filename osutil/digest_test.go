// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package osutil_test

import (
	"crypto"
	"crypto/sha512"
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type FileDigestSuite struct{}

var _ = Suite(&FileDigestSuite{})

func (ts *FileDigestSuite) TestFileDigest(c *C) {
	exData := []byte("hashmeplease")

	tempdir := c.MkDir()
	fn := filepath.Join(tempdir, "ex.file")
	err := ioutil.WriteFile(fn, exData, 0644)
	c.Assert(err, IsNil)

	digest, size, err := osutil.FileDigest(fn, crypto.SHA512)
	c.Assert(err, IsNil)
	c.Check(size, Equals, uint64(len(exData)))
	h512 := sha512.Sum512(exData)
	c.Check(digest, DeepEquals, h512[:])
}

type testPartialFileDigestData struct {
	data   []byte
	offset uint64
}

func (ts *FileDigestSuite) testPartialFileDigest(c *C, data *testPartialFileDigestData) error {
	tempdir := c.MkDir()
	fn := filepath.Join(tempdir, "ex.file")
	err := ioutil.WriteFile(fn, data.data, 0644)
	c.Assert(err, IsNil)

	digest, size, err := osutil.PartialFileDigest(fn, crypto.SHA512, uint64(data.offset))
	if err != nil {
		return err
	}

	c.Check(size, Equals, uint64(len(data.data[data.offset:])))
	h512 := sha512.Sum512(data.data[data.offset:])
	c.Check(digest, DeepEquals, h512[:])

	return nil
}

func (ts *FileDigestSuite) TestPartialFileDigest(c *C) {
	exData := []byte("hashmeplease")
	err := ts.testPartialFileDigest(c, &testPartialFileDigestData{
		data:   exData,
		offset: 1,
	})
	c.Assert(err, IsNil)
}

func (ts *FileDigestSuite) TestPartialFileDigestInvalidOffset(c *C) {
	exData := []byte("hashmeplease")
	err := ts.testPartialFileDigest(c, &testPartialFileDigestData{
		data:   exData,
		offset: uint64(len(exData) + 1),
	})
	c.Check(err, ErrorMatches, "offset exceeds file size")
}
