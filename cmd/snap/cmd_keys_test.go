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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/store"
)

type SnapKeysSuite struct {
	BaseSnapSuite

	GnupgCmd string
	tempdir  string
}

// FIXME: Ideally we would just use gpg2 and remove the gnupg2_test.go file.
// However currently there is LP: #1621839 which prevents us from switching to
// gpg2 fully. Once this is resolved we should switch.
var _ = Suite(&SnapKeysSuite{GnupgCmd: "/usr/bin/gpg"})

var fakePinentryData = []byte(`#!/bin/sh
set -e
echo "OK Pleased to meet you"
while true; do
  read line
  case $line in
  BYE)
    exit 0
  ;;
  *)
    echo "OK I agree to everything"
    ;;
esac
done
`)

func (s *SnapKeysSuite) SetUpTest(c *C) {
	if testing.Short() && s.GnupgCmd == "/usr/bin/gpg2" {
		c.Skip("gpg2 does not do short tests")
	}
	s.BaseSnapSuite.SetUpTest(c)

	s.tempdir = c.MkDir()
	for _, fileName := range []string{"pubring.gpg", "secring.gpg", "trustdb.gpg"} {
		data, err := ioutil.ReadFile(filepath.Join("test-data", fileName))
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(s.tempdir, fileName), data, 0644)
		c.Assert(err, IsNil)
	}
	fakePinentryFn := filepath.Join(s.tempdir, "pinentry-fake")
	err := os.WriteFile(fakePinentryFn, fakePinentryData, 0755)
	c.Assert(err, IsNil)
	gpgAgentConfFn := filepath.Join(s.tempdir, "gpg-agent.conf")
	err = os.WriteFile(gpgAgentConfFn, []byte(fmt.Sprintf(`pinentry-program %s`, fakePinentryFn)), 0644)
	c.Assert(err, IsNil)

	os.Setenv("SNAP_GNUPG_HOME", s.tempdir)
	os.Setenv("SNAP_GNUPG_CMD", s.GnupgCmd)

	// by default avoid talking to the real store
	s.AddCleanup(snap.MockStoreNew(func(cfg *store.Config, stoCtx store.DeviceAndAuthContext) *store.Store {
		if cfg == nil {
			cfg = store.DefaultConfig()
		}
		serverURL, _ := url.Parse("http://nowhere.example.com")
		cfg.AssertionsBaseURL = serverURL
		return store.New(cfg, stoCtx)
	}))
}

func (s *SnapKeysSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAP_GNUPG_HOME")
	os.Unsetenv("SNAP_GNUPG_CMD")
	s.BaseSnapSuite.TearDownTest(c)
}

func (s *SnapKeysSuite) TestKeys(c *C) {
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"keys"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches, `Name +SHA3-384
default +g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ
another +DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestKeysEmptyNoHeader(c *C) {
	// simulate empty keys
	err := os.RemoveAll(s.tempdir)
	c.Assert(err, IsNil)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"keys"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "No keys registered, see `snapcraft create-key`\n")
}

func (s *SnapKeysSuite) TestKeysJSON(c *C) {
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"keys", "--json"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedResponse := []snap.Key{
		{
			Name:     "default",
			Sha3_384: "g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ",
		},
		{
			Name:     "another",
			Sha3_384: "DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L",
		},
	}
	var obtainedResponse []snap.Key
	json.Unmarshal(s.stdout.Bytes(), &obtainedResponse)
	c.Check(obtainedResponse, DeepEquals, expectedResponse)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestKeysJSONEmpty(c *C) {
	err := os.RemoveAll(os.Getenv("SNAP_GNUPG_HOME"))
	c.Assert(err, IsNil)
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"keys", "--json"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "[]\n")
	c.Check(s.Stderr(), Equals, "")
}
