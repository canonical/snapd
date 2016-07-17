// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"

	snap "github.com/snapcore/snapd/cmd/snap"
)

type snapsignSuite struct {
	BaseSnapSuite

	tempdir string
	homedir string
}

var _ = Suite(&snapsignSuite{})

func (s *snapsignSuite) SetUpSuite(c *C) {
	s.tempdir = c.MkDir()
	s.homedir = filepath.Join(s.tempdir, "gpg")
	err := os.Mkdir(s.homedir, 0700)
	c.Assert(err, IsNil)

	assertstest.GPGImportKey(s.homedir, assertstest.DevKey)
}

var statement = []byte(fmt.Sprintf(`type: snap-build
authority-id: devel1
series: "16"
snap-id: snapidsnapidsnapidsnapidsnapidsn
snap-digest: sha512-pKvURIxJVi2CgRXROh_M6pJ_UrTVRZKX-LQ-QtqJI4vBNibkPcs43bCCSIkn7JBPtCBXRDmD6IWFF51QVRr-Yg
snap-size: 1
grade: devel
timestamp: %s
`, time.Now().Format(time.RFC3339)))

func (s *snapsignSuite) TestHappy(c *C) {
	s.stdin.Write(statement)

	rest, err := snap.Parser().ParseArgs([]string{"sign", "--gpg-homedir", s.homedir, "--key-id", assertstest.DevKeyID})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}

func (s *snapsignSuite) TestHappyAccountKeyHandle(c *C) {
	accKeyFile := filepath.Join(s.tempdir, "devel1.account-key")

	devKey, _ := assertstest.ReadPrivKey(assertstest.DevKey)
	pubKeyEncoded, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)

	now := time.Now()
	// good enough as a handle as is used by Sign
	mockAccKey := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: devel1\n" +
		"public-key-id: " + assertstest.DevKeyID + "\n" +
		"public-key-fingerprint: " + assertstest.DevKeyFingerprint + "\n" +
		"since: " + now.Format(time.RFC3339) + "\n" +
		"until: " + now.AddDate(1, 0, 0).Format(time.RFC3339) + "\n" +
		fmt.Sprintf("body-length: %v", len(pubKeyEncoded)) + "\n\n" +
		string(pubKeyEncoded) + "\n\n" +
		"openpgp c2ln"

	err = ioutil.WriteFile(accKeyFile, []byte(mockAccKey), 0655)
	c.Assert(err, IsNil)

	s.stdin.Write(statement)

	rest, err := snap.Parser().ParseArgs([]string{"sign", "--gpg-homedir", s.homedir, "--account-key", accKeyFile})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}
