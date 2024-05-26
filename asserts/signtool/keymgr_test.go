// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package signtool_test

import (
	"os"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/testutil"
)

type keymgrSuite struct{}

var _ = check.Suite(&keymgrSuite{})

func (keymgrSuite) TestGPGKeypairManager(c *check.C) {
	keypairMgr := mylog.Check2(signtool.GetKeypairManager())
	c.Check(err, check.IsNil)
	c.Check(keypairMgr, check.FitsTypeOf, &asserts.GPGKeypairManager{})
}

func mockNopExtKeyMgr(c *check.C) (pgm *testutil.MockCmd, restore func()) {
	os.Setenv("SNAPD_EXT_KEYMGR", "keymgr")
	pgm = testutil.MockCommand(c, "keymgr", `
if [ "$1" == "features" ]; then
  echo '{"signing":["RSA-PKCS"] , "public-keys":["DER"]}'
  exit 0
fi
exit 1
`)
	r := func() {
		pgm.Restore()
		os.Unsetenv("SNAPD_EXT_KEYMGR")
	}

	return pgm, r
}

func (keymgrSuite) TestExternalKeypairManager(c *check.C) {
	pgm, restore := mockNopExtKeyMgr(c)
	defer restore()

	keypairMgr := mylog.Check2(signtool.GetKeypairManager())
	c.Check(err, check.IsNil)
	c.Check(keypairMgr, check.FitsTypeOf, &asserts.ExternalKeypairManager{})
	c.Check(pgm.Calls(), check.HasLen, 1)
}

func (keymgrSuite) TestExternalKeypairManagerError(c *check.C) {
	os.Setenv("SNAPD_EXT_KEYMGR", "keymgr")
	defer os.Unsetenv("SNAPD_EXT_KEYMGR")

	pgm := testutil.MockCommand(c, "keymgr", `
exit 1
`)
	defer pgm.Restore()

	_ := mylog.Check2(signtool.GetKeypairManager())
	c.Check(err, check.ErrorMatches, `cannot setup external keypair manager: external keypair manager "keymgr" \[features\] failed: exit status 1.*`)
}

func (keymgrSuite) TestExternalKeypairManagerGenerateKey(c *check.C) {
	_, restore := mockNopExtKeyMgr(c)
	defer restore()

	keypairMgr := mylog.Check2(signtool.GetKeypairManager())
	c.Check(err, check.IsNil)
	mylog.Check(signtool.GenerateKey(keypairMgr, "key"))
	c.Check(err, check.ErrorMatches, `cannot generate external keypair manager key via snap command, use the appropriate external procedure to create a 4096-bit RSA key under the name/label "key"`)
}
