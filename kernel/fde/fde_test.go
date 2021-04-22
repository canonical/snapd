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

package fde_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/secboot"
)

func TestFde(t *testing.T) { TestingT(t) }

type fdeSuite struct{}

var _ = Suite(&fdeSuite{})

func (s *fdeSuite) TestHasRevealKey(c *C) {
	oldPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", oldPath) }()

	mockRoot := c.MkDir()
	os.Setenv("PATH", mockRoot+"/bin")
	mockBin := mockRoot + "/bin/"
	err := os.Mkdir(mockBin, 0755)
	c.Assert(err, IsNil)

	// no fde-reveal-key binary
	c.Check(fde.HasRevealKey(), Equals, false)

	// fde-reveal-key without +x
	err = ioutil.WriteFile(mockBin+"fde-reveal-key", nil, 0644)
	c.Assert(err, IsNil)
	c.Check(fde.HasRevealKey(), Equals, false)

	// correct fde-reveal-key, no logging
	err = os.Chmod(mockBin+"fde-reveal-key", 0755)
	c.Assert(err, IsNil)
}

func (s *fdeSuite) TestInitialSetupV2(c *C) {
	mockKey := secboot.EncryptionKey{1, 2, 3, 4}

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:      "initial-setup",
			Key:     mockKey[:],
			KeyName: "some-key-name",
		})
		// sealed-key/handle
		mockJSON := fmt.Sprintf(`{"sealed-key":"%s", "handle":{"some":"handle"}}`, base64.StdEncoding.EncodeToString([]byte("the-encrypted-key")))
		return []byte(mockJSON), nil
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey[:],
		KeyName: "some-key-name",
	}
	res, err := fde.InitialSetup(runSetupHook, params)
	c.Assert(err, IsNil)
	expectedHandle := json.RawMessage([]byte(`{"some":"handle"}`))
	c.Check(res, DeepEquals, &fde.InitialSetupResult{
		EncryptedKey: []byte("the-encrypted-key"),
		Handle:       &expectedHandle,
	})
}

func (s *fdeSuite) TestInitialSetupError(c *C) {
	mockKey := secboot.EncryptionKey{1, 2, 3, 4}

	errHook := errors.New("hook running error")
	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:      "initial-setup",
			Key:     mockKey[:],
			KeyName: "some-key-name",
		})
		return nil, errHook
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey[:],
		KeyName: "some-key-name",
	}
	_, err := fde.InitialSetup(runSetupHook, params)
	c.Check(err, Equals, errHook)
}

func (s *fdeSuite) TestInitialSetupV1(c *C) {
	mockKey := secboot.EncryptionKey{1, 2, 3, 4}

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:      "initial-setup",
			Key:     mockKey[:],
			KeyName: "some-key-name",
		})
		// needs the USK$ prefix to simulate v1 key
		return []byte("USK$sealed-key"), nil
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey[:],
		KeyName: "some-key-name",
	}
	res, err := fde.InitialSetup(runSetupHook, params)
	c.Assert(err, IsNil)
	expectedHandle := json.RawMessage(`{v1-no-handle: true}`)
	c.Check(res, DeepEquals, &fde.InitialSetupResult{
		EncryptedKey: []byte("USK$sealed-key"),
		Handle:       &expectedHandle,
	})
}

func (s *fdeSuite) TestInitialSetupBadJSON(c *C) {
	mockKey := secboot.EncryptionKey{1, 2, 3, 4}

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		return []byte("bad json"), nil
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey[:],
		KeyName: "some-key-name",
	}
	_, err := fde.InitialSetup(runSetupHook, params)
	c.Check(err, ErrorMatches, `cannot decode hook output "bad json": invalid char.*`)
}
