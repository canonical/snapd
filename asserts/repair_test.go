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
	"time"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

var (
	_ = Suite(&repairSuite{})
)

type repairSuite struct {
	until     time.Time
	untilLine string
	since     time.Time
	sinceLine string

	modelsLine string

	repairStr string
}

const script = `#!/bin/sh
    set -e
    echo "Unpack embedded payload"
    match=$(grep --text --line-number '^PAYLOAD:$' $0 | cut -d ':' -f 1)
    payload_start=$((match + 1))
    tail -n +$payload_start $0 | uudecode | tar -xzf -
    # run embedded content
    ./hello
    exit 0
    # payload generated with, may contain binary data
    #   printf '#!/bin/sh\necho hello from the inside\n' > hello
    #   chmod +x hello
    #   tar czvf - hello | uuencode --base64 -
    PAYLOAD:
    begin-base64 644 -
    H4sIAJl991gAA+3SSwrCMBSF4Yy7iisuoAkxyXp8RBOoDTR1/6Y6EQQdFRH+
    b3IG9wzO4KY4DEWtSzfBuSVNcPo1n3ZOGdsqPjhrW89o570SvfKuh1ud95OI
    ipcyfup9u/+p7aY/5LGvqYvHVCQt7yDnqVxlTlHyWPMpdr8eCQAAAAAAAAAA
    AAAAAAB4cwdxEVGzACgAAA==
    ====
`

const repairExample = "type: repair\n" +
	"authority-id: canonical\n" +
	"repair-id: REPAIR-42\n" +
	"series:\n" +
	"  - 16\n" +
	"MODELSLINE\n" +
	"script:\n" +
	"    SCRIPT\n" +
	"SINCELINE\n" +
	"UNTILLINE\n" +
	"body-length: 0\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"AXNpZw=="

func (em *repairSuite) SetUpTest(c *C) {
	em.since = time.Now().Truncate(time.Second)
	em.sinceLine = fmt.Sprintf("since: %s\n", em.since.Format(time.RFC3339))
	em.until = time.Now().AddDate(0, 1, 0).Truncate(time.Second)
	em.untilLine = fmt.Sprintf("until: %s\n", em.until.Format(time.RFC3339))
	em.modelsLine = "models:\n  - frobinator\n"
	em.repairStr = strings.Replace(repairExample, "UNTILLINE\n", em.untilLine, 1)
	em.repairStr = strings.Replace(em.repairStr, "SINCELINE\n", em.sinceLine, 1)
	em.repairStr = strings.Replace(em.repairStr, "MODELSLINE\n", em.modelsLine, 1)
	em.repairStr = strings.Replace(em.repairStr, "SCRIPT\n", script, 1)
}

func (em *repairSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(em.repairStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)
	c.Check(repair.RepairID(), Equals, "REPAIR-42")
	c.Check(repair.Series(), DeepEquals, []string{"16"})
	c.Check(repair.Models(), DeepEquals, []string{"frobinator"})
	c.Check(repair.Script(), Equals, strings.TrimSpace(strings.Replace(script, "    ", "", -1)))
	c.Check(repair.Since().Equal(em.since), Equals, true)
	c.Check(repair.Until().Equal(em.until), Equals, true)
}

const (
	repairErrPrefix = "assertion repair: "
)

func (em *repairSuite) TestDecodeInvalid(c *C) {
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series:\n  - 16\n", "series: \n", `"series" header must be a list of strings`},
		{"series:\n  - 16\n", "series: something\n", `"series" header must be a list of strings`},
		{"models:\n  - frobinator\n", "models: \n", `"models" header must be a list of strings`},
		{"models:\n  - frobinator\n", "models: something\n", `"models" header must be a list of strings`},
		{em.sinceLine, "since: \n", `"since" header should not be empty`},
		{em.sinceLine, "since: 12:30\n", `"since" header is not a RFC3339 date: .*`},
		{em.untilLine, "until: \n", `"until" header should not be empty`},
		{em.untilLine, "until: 12:30\n", `"until" header is not a RFC3339 date: .*`},
		{em.untilLine, "until: 1002-11-01T22:08:41+00:00\n", `'until' time cannot be before 'since' time`},
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
	c.Assert(err, IsNil)
	c.Check(string(output), Equals, `Unpack embedded payload
hello from the inside
`)
}
