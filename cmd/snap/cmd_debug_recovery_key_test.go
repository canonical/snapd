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
	"encoding/hex"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
)

func (s *SnapSuite) TestDebugRecoveryKeyOnClassicErrors(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "show-recovery-key"})
	c.Assert(err, ErrorMatches, `cannot use the "show-recovery-key" command is not available on classic systems`)
}

func makeMockRecoveryKeyFile(c *C, rkeybuf []byte) {
	mockRecoveryKeyFile := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	err := os.MkdirAll(filepath.Dir(mockRecoveryKeyFile), 0755)
	c.Assert(err, IsNil)

	if rkeybuf != nil {
		err = ioutil.WriteFile(mockRecoveryKeyFile, rkeybuf, 0600)
		c.Assert(err, IsNil)
	} else {
		os.Remove(mockRecoveryKeyFile)
	}
}

func (s *SnapSuite) TestDebugRecoveryKeyHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// same inputs/outputs as secboot:crypt_test.go in this test
	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	makeMockRecoveryKeyFile(c, rkeystr)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "show-recovery-key"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "61665-00531-54469-09783-47273-19035-40077-28287\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugRecoveryKeyUnhappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, tc := range []struct {
		rkey        []byte
		expectedErr string
	}{
		{nil, `cannot open recovery key: open .*/recovery.key: no such file or directory`},
		{[]byte{}, `cannot read recovery key: unexpected size 0 for the recovery key file`},
		{[]byte{0, 1}, `cannot read recovery key: unexpected size 2 for the recovery key file`},
		{[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}, `cannot read recovery key: unexpected size 17 for the recovery key file`},
	} {
		makeMockRecoveryKeyFile(c, tc.rkey)
		_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "show-recovery-key"})
		c.Assert(err, ErrorMatches, tc.expectedErr)
	}
}
