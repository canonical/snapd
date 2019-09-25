// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package volmgr_test

import (
	"path"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-install/volmgr"
	"github.com/snapcore/snapd/testutil"
)

func (s *volmgrTestSuite) TestEncryptedPartitionCreate(c *C) {
	c.Assert(volmgr.MasterKeySize, Equals, 64)

	cmd := testutil.MockCommand(c, "cryptsetup", "exit 0")
	defer cmd.Restore()

	*volmgr.TempKeyFile = path.Join(c.MkDir(), "unlock.tmp")
	key := make([]byte, volmgr.MasterKeySize)
	v := volmgr.NewEncryptedPartition("/dev/node", "crypt_name", key)
	err := v.Create()
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cryptsetup", "-q", "luksFormat", "--type", "luks2", "--pbkdf-memory", "1000", "--master-key-file", *volmgr.TempKeyFile, "/dev/node"},
		{"cryptsetup", "open", "--master-key-file", *volmgr.TempKeyFile, "/dev/node", "crypt_name"},
	})
	c.Assert(*volmgr.TempKeyFile, testutil.FileAbsent)
}
