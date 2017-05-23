// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package asserts_test

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

var (
	_ = Suite(&repairSuite{})
)

type repairSuite struct {
	modelsLine string

	repairStr string
}

const script = `#!/bin/sh
set -e
echo "Unpack embedded payload"
match=$(grep --text --line-number '^PAYLOAD:$' $0 | cut -d ':' -f 1)
payload_start=$((match + 1))
# Using "base64" as its part of coreutils which should be available
# everywhere
tail -n +$payload_start $0 | base64 --decode - | tar -xzf -
# run embedded content
./hello
exit 0
# payload generated with, may contain binary data
#   printf '#!/bin/sh\necho hello from the inside\n' > hello
#   chmod +x hello
#   tar czf - hello | base64 -
PAYLOAD:
H4sIAJJt+FgAA+3STQrCMBDF8ax7ihEP0CkxyXn8iCZQE2jr/W11Iwi6KiL8f5u3mLd4i0mx76tZ
l86Cc0t2welrPu2c6awGr95bG4x26rw1oivveriN034QMfFSy6fet/uf2m7aQy7tmJp4TFXS8g5y
HupVphQllzGfYvPrkQAAAAAAAAAAAAAAAACAN3dTp9TNACgAAA==
`

var repairExample = fmt.Sprintf("type: repair\n"+
	"authority-id: acme\n"+
	"brand-id: acme\n"+
	"architectures:\n"+
	"  - amd64\n"+
	"  - arm64\n"+
	"repair-id: 42\n"+
	"series:\n"+
	"  - 16\n"+
	"MODELSLINE\n"+
	"body-length: %v\n"+
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij"+
	"\n\n"+
	script+"\n\n"+
	"AXNpZw==", len(script))

func (s *repairSuite) SetUpTest(c *C) {
	s.modelsLine = "models:\n  - acme/frobinator\n"
	s.repairStr = strings.Replace(repairExample, "MODELSLINE\n", s.modelsLine, 1)
	s.repairStr = strings.Replace(s.repairStr, "SCRIPT\n", script, 1)
}

func (s *repairSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(s.repairStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)
	c.Check(repair.BrandID(), Equals, "acme")
	c.Check(repair.RepairID(), Equals, "42")
	c.Check(repair.Series(), DeepEquals, []string{"16"})
	c.Check(repair.Architectures(), DeepEquals, []string{"amd64", "arm64"})
	c.Check(repair.Models(), DeepEquals, []string{"acme/frobinator"})
	c.Check(string(repair.Body()), Equals, script)
}

const (
	repairErrPrefix = "assertion repair: "
)

func (s *repairSuite) TestDecodeInvalid(c *C) {
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series:\n  - 16\n", "series: \n", `"series" header must be a list of strings`},
		{"series:\n  - 16\n", "series: something\n", `"series" header must be a list of strings`},
		{"architectures:\n  - amd64\n  - arm64\n", "architectures: foo\n", `"architectures" header must be a list of strings`},
		{"models:\n  - acme/frobinator\n", "models: \n", `"models" header must be a list of strings`},
		{"models:\n  - acme/frobinator\n", "models: something\n", `"models" header must be a list of strings`},
		{"repair-id: 42\n", "repair-id: no-number\n", `"repair-id" header contains invalid characters: "no-number"`},
		{"repair-id: 42\n", "repair-id: 0\n", `"repair-id" header contains invalid characters: "0"`},
		{"repair-id: 42\n", "repair-id: 01\n", `"repair-id" header contains invalid characters: "01"`},
		{"brand-id: acme\n", "brand-id: brand-id-not-eq-authority-id\n", `authority-id and brand-id must match, repair assertions are expected to be signed by the brand: "acme" != "brand-id-not-eq-authority-id"`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(s.repairStr, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, repairErrPrefix+test.expectedErr)
	}
}

// FIXME: move to a different layer later
func (s *repairSuite) TestRepairCanEmbeddScripts(c *C) {
	a, err := asserts.Decode([]byte(s.repairStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)

	tmpdir := c.MkDir()
	repairScript := filepath.Join(tmpdir, "repair")
	err = ioutil.WriteFile(repairScript, []byte(repair.Body()), 0755)
	c.Assert(err, IsNil)
	cmd := exec.Command(repairScript)
	cmd.Dir = tmpdir
	output, err := cmd.CombinedOutput()
	c.Check(err, IsNil)
	c.Check(string(output), Equals, `Unpack embedded payload
hello from the inside
`)
}
