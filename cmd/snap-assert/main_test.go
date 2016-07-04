// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"

	snapassert "github.com/snapcore/snapd/cmd/snap-assert"
)

func Test(t *testing.T) { TestingT(t) }

type snapassertSuite struct {
	tempdir string
	homedir string

	savedArgs []string

	stdin  *bytes.Buffer
	stdout *bytes.Buffer
}

var _ = Suite(&snapassertSuite{})

func (s *snapassertSuite) SetUpSuite(c *C) {
	s.tempdir = c.MkDir()
	s.homedir = filepath.Join(s.tempdir, "gpg")
	err := os.Mkdir(s.homedir, 0700)
	c.Assert(err, IsNil)

	assertstest.GPGImportKey(s.homedir, assertstest.DevKey)
}

func (s *snapassertSuite) SetUpTest(c *C) {
	s.savedArgs = os.Args
	s.stdin = new(bytes.Buffer)
	s.stdout = new(bytes.Buffer)
	snapassert.Stdout = s.stdout
	snapassert.Stdin = s.stdin
}

func (s *snapassertSuite) TearDownTest(c *C) {
	snapassert.Stdin = os.Stdin
	snapassert.Stdout = os.Stdout
	os.Args = s.savedArgs
}

func (s *snapassertSuite) TestHappy(c *C) {
	os.Args = []string{"", "--gpg-homedir", s.homedir, "--key-id", assertstest.DevKeyID, "--authority-id", "devel1", "snap-build"}

	s.stdin.Write([]byte(fmt.Sprintf(`series: "16"
snap-id: snapidsnapidsnapidsnapidsnapidsn
snap-digest: sha512-pKvURIxJVi2CgRXROh_M6pJ_UrTVRZKX-LQ-QtqJI4vBNibkPcs43bCCSIkn7JBPtCBXRDmD6IWFF51QVRr-Yg
snap-size: 1
grade: devel
timestamp: %s
`, time.Now().Format(time.RFC3339))))

	err := snapassert.Run()
	c.Assert(err, IsNil)

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}

func (s *snapassertSuite) TestHappyJSONAccountKeyStatementFile(c *C) {
	accKeyFile := filepath.Join(s.tempdir, "devel1.account-key")
	statementFile := filepath.Join(s.tempdir, "snap-build")

	devKey, _ := assertstest.ReadPrivKey(assertstest.DevKey)
	pubKeyEncoded, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)

	now := time.Now()
	// good enough as a handle as is used by Sign
	mockAccKey := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: user-id1\n" +
		"public-key-id: " + assertstest.DevKeyID + "\n" +
		"public-key-fingerprint: " + assertstest.DevKeyFingerprint + "\n" +
		"since: " + now.Format(time.RFC3339) + "\n" +
		"until: " + now.AddDate(1, 0, 0).Format(time.RFC3339) + "\n" +
		fmt.Sprintf("body-length: %v", len(pubKeyEncoded)) + "\n\n" +
		string(pubKeyEncoded) + "\n\n" +
		"openpgp c2ln"

	err = ioutil.WriteFile(accKeyFile, []byte(mockAccKey), 0655)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"series":      "16",
		"snap-id":     "snapidsnapidsnapidsnapidsnapidsn",
		"snap-digest": "sha512-pKvURIxJVi2CgRXROh_M6pJ_UrTVRZKX-LQ-QtqJI4vBNibkPcs43bCCSIkn7JBPtCBXRDmD6IWFF51QVRr-Yg",
		"snap-size":   1,
		"grade":       "devel",
		"timestamp":   now.Format(time.RFC3339),
	}

	b, err := json.Marshal(headers)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(statementFile, b, 0655)
	c.Assert(err, IsNil)

	os.Args = []string{"", "--gpg-homedir", s.homedir, "--format", "json", "--account-key", accKeyFile, "snap-build", statementFile}

	err = snapassert.Run()
	c.Assert(err, IsNil)

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}
