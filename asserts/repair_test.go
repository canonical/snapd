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

const repairExample = "type: repair\n" +
	"authority-id: canonical\n" +
	"repair-id: REPAIR-42\n" +
	"series:\n" +
	"  - 16\n" +
	"MODELSLINE\n" +
	"script:\n" +
	"    SCRIPT\n" +
	"body-length: 0\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"AXNpZw=="

func (em *repairSuite) SetUpTest(c *C) {
	em.modelsLine = "models:\n  - acme/frobinator\n"
	em.repairStr = strings.Replace(repairExample, "MODELSLINE\n", em.modelsLine, 1)
	em.repairStr = strings.Replace(em.repairStr, "SCRIPT\n", script, 1)
}

func (em *repairSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(em.repairStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)
	c.Check(repair.RepairID(), Equals, "REPAIR-42")
	c.Check(repair.Series(), DeepEquals, []string{"16"})
	c.Check(repair.Models(), DeepEquals, []string{"acme/frobinator"})
	c.Check(repair.Script(), Equals, strings.TrimSpace(strings.Replace(script, "    ", "", -1)))
}

const (
	repairErrPrefix = "assertion repair: "
)

func (em *repairSuite) TestDecodeInvalid(c *C) {
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series:\n  - 16\n", "series: \n", `"series" header must be a list of strings`},
		{"series:\n  - 16\n", "series: something\n", `"series" header must be a list of strings`},
		{"models:\n  - acme/frobinator\n", "models: \n", `"models" header must be a list of strings`},
		{"models:\n  - acme/frobinator\n", "models: something\n", `"models" header must be a list of strings`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(em.repairStr, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, repairErrPrefix+test.expectedErr)
	}
}

// FIXME: move to a different layer later
func (em *repairSuite) TestRepairCanEmbeddScripts(c *C) {
	a, err := asserts.Decode([]byte(em.repairStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)

	tmpdir := c.MkDir()
	repairScript := filepath.Join(tmpdir, "repair")
	err = ioutil.WriteFile(repairScript, []byte(repair.Script()), 0755)
	c.Assert(err, IsNil)
	cmd := exec.Command(repairScript)
	cmd.Dir = tmpdir
	output, err := cmd.CombinedOutput()
	c.Check(err, IsNil)
	c.Check(string(output), Equals, `Unpack embedded payload
hello from the inside
`)
}
