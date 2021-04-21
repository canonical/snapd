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

package fde_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/devicestate/fde"
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

func (s *fdeSuite) TestUnmarshalFDESetupResultSad(c *C) {
	_, err := fde.UnmarshalFDESetupResult([]byte(`bad json`))
	c.Check(err, ErrorMatches, `cannot decode hook output "bad json": invalid char.*`)
}

func (s *fdeSuite) TestUnmarshalFDESetupResultCompatSecbootV1(c *C) {
	res, err := fde.UnmarshalFDESetupResult([]byte(`USK$old-v1-secboot-generated-key`))
	c.Check(err, IsNil)
	handle := json.RawMessage(`{v1-no-handle: true}`)
	c.Check(res, DeepEquals, &boot.FDESetupHookResult{
		EncryptedKey: []byte("USK$old-v1-secboot-generated-key"),
		Handle:       &handle,
	})
}

func (s *fdeSuite) TestUnmarshalFDESetupResultHappy(c *C) {
	res, err := fde.UnmarshalFDESetupResult([]byte(fmt.Sprintf(`{"encrypted-key":"%s","handle":{"the":"handle","is":"raw-json"}}`, base64.StdEncoding.EncodeToString([]byte("the-key")))))
	c.Check(err, IsNil)
	handle := json.RawMessage(`{"the":"handle","is":"raw-json"}`)
	c.Check(res, DeepEquals, &boot.FDESetupHookResult{
		EncryptedKey: []byte("the-key"),
		Handle:       &handle,
	})
}
