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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type FileDigestSuite struct{}

var _ = Suite(&FileDigestSuite{})

func (ts *FileDigestSuite) TestFileDigest(c *C) {
	exData := []byte("hashmeplease")

	tempdir := c.MkDir()
	fn := filepath.Join(tempdir, "ex.file")
	mylog.Check(os.WriteFile(fn, exData, 0644))


	digest, size := mylog.Check3(osutil.FileDigest(fn, crypto.SHA512))

	c.Check(size, Equals, uint64(len(exData)))
	h512 := sha512.Sum512(exData)
	c.Check(digest, DeepEquals, h512[:])
}
