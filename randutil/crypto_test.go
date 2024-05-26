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

package randutil_test

import (
	"encoding/base64"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/randutil"
)

type cryptoRandutilSuite struct{}

var _ = Suite(&cryptoRandutilSuite{})

func (s *cryptoRandutilSuite) TestCryptoTokenBytes(c *C) {
	x := mylog.Check2(randutil.CryptoTokenBytes(5))

	c.Check(x, HasLen, 5)
}

func (s *cryptoRandutilSuite) TestCryptoToken(c *C) {
	x := mylog.Check2(randutil.CryptoToken(5))


	b := mylog.Check2(base64.RawURLEncoding.DecodeString(x))

	c.Check(b, HasLen, 5)
}

var kernelTestUUID = "1031319a-b661-4c01-aafa-6def8a118944"

func (s *cryptoRandutilSuite) TestRandomKernelUUIDNoFile(c *C) {
	uuidPath := filepath.Join(c.MkDir(), "no-file")
	defer randutil.MockKernelUUIDPath(uuidPath)()

	value := mylog.Check2(randutil.RandomKernelUUID())
	c.Check(value, Equals, "")
	c.Check(err, ErrorMatches,
		"cannot read kernel generated uuid:"+
			".*no-file: no such file or directory")
}

func (s *cryptoRandutilSuite) TestRandomKernelUUIDNoPerm(c *C) {
	if os.Getuid() == 0 {
		c.Skip("Permission tests will not work when user is root")
	}

	uuidPath := filepath.Join(c.MkDir(), "no-perm")
	defer randutil.MockKernelUUIDPath(uuidPath)()
	mylog.Check(os.WriteFile(uuidPath, []byte(kernelTestUUID), 0))


	value := mylog.Check2(randutil.RandomKernelUUID())
	c.Check(value, Equals, "")
	c.Assert(err, ErrorMatches,
		"cannot read kernel generated uuid:"+
			".*no-perm: permission denied")
}

func (s *cryptoRandutilSuite) TestRandomKernelUUID(c *C) {
	for _, uuid := range []string{
		kernelTestUUID,
		" \t\n " + kernelTestUUID + " \n\t\r\n",
	} {
		// Create new path on each iteration because we cannot
		// reuse previous path to read-only (0444) file.
		uuidPath := filepath.Join(c.MkDir(), "uuid")
		defer randutil.MockKernelUUIDPath(uuidPath)()
		mylog.Check(os.WriteFile(uuidPath, []byte(uuid), 0444))


		value := mylog.Check2(randutil.RandomKernelUUID())
		c.Check(value, Equals, kernelTestUUID)

	}
}

func (s *cryptoRandutilSuite) TestRandomKernelUUIDReal(c *C) {
	mylog.Check2(os.Stat(randutil.KernelUUIDPath))

	value := mylog.Check2(randutil.RandomKernelUUID())
	c.Check(value, Not(Equals), "")
	// https://www.rfc-editor.org/rfc/rfc4122#section-3
	// We are not testing the kernel here, so minimal check:
	// UUID should be 36 bytes in length exactly.
	c.Check(value, HasLen, 36)

}
