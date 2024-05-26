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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

var _ = Suite(&repairSuite{})

type repairSuite struct {
	modelsLine string
	ts         time.Time
	tsLine     string
	basesLine  string
	modesLine  string

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
	"summary: example repair\n"+
	"architectures:\n"+
	"  - amd64\n"+
	"  - arm64\n"+
	"repair-id: 42\n"+
	"series:\n"+
	"  - 16\n"+
	"MODELSLINE"+
	"TSLINE"+
	"BASESLINE"+
	"MODESLINE"+
	"body-length: %v\n"+
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij"+
	"\n\n"+
	script+"\n\n"+
	"AXNpZw==", len(script))

func (s *repairSuite) SetUpTest(c *C) {
	s.modelsLine = "models:\n  - acme/frobinator\n"
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
	s.basesLine = "bases:\n  - core20\n"
	s.modesLine = "modes:\n  - run\n"

	s.repairStr = strings.Replace(repairExample, "MODELSLINE", s.modelsLine, 1)
	s.repairStr = strings.Replace(s.repairStr, "TSLINE", s.tsLine, 1)
	s.repairStr = strings.Replace(s.repairStr, "BASESLINE", s.basesLine, 1)
	s.repairStr = strings.Replace(s.repairStr, "MODESLINE", s.modesLine, 1)
}

func (s *repairSuite) TestDecodeOK(c *C) {
	a := mylog.Check2(asserts.Decode([]byte(s.repairStr)))

	c.Check(a.Type(), Equals, asserts.RepairType)
	_, ok := a.(asserts.SequenceMember)
	c.Assert(ok, Equals, true)
	repair := a.(*asserts.Repair)
	c.Check(repair.Timestamp(), Equals, s.ts)
	c.Check(repair.BrandID(), Equals, "acme")
	c.Check(repair.RepairID(), Equals, 42)
	c.Check(repair.Sequence(), Equals, 42)
	c.Check(repair.Summary(), Equals, "example repair")
	c.Check(repair.Series(), DeepEquals, []string{"16"})
	c.Check(repair.Bases(), DeepEquals, []string{"core20"})
	c.Check(repair.Modes(), DeepEquals, []string{"run"})
	c.Check(repair.Architectures(), DeepEquals, []string{"amd64", "arm64"})
	c.Check(repair.Models(), DeepEquals, []string{"acme/frobinator"})
	c.Check(string(repair.Body()), Equals, script)
}

const (
	repairErrPrefix = "assertion repair: "
)

func (s *repairSuite) TestDisabled(c *C) {
	disabledTests := []struct {
		disabled, expectedErr string
		dis                   bool
	}{
		{"true", "", true},
		{"false", "", false},
		{"foo", `"disabled" header must be 'true' or 'false'`, false},
	}

	for _, test := range disabledTests {
		repairStr := strings.Replace(repairExample, "MODELSLINE", fmt.Sprintf("disabled: %s\n", test.disabled), 1)
		repairStr = strings.Replace(repairStr, "TSLINE", s.tsLine, 1)
		repairStr = strings.Replace(repairStr, "BASESLINE", "", 1)
		repairStr = strings.Replace(repairStr, "MODESLINE", "", 1)

		a := mylog.Check2(asserts.Decode([]byte(repairStr)))
		if test.expectedErr != "" {
			c.Check(err, ErrorMatches, repairErrPrefix+test.expectedErr)
		} else {

			repair := a.(*asserts.Repair)
			c.Check(repair.Disabled(), Equals, test.dis)
		}
	}
}

func (s *repairSuite) TestDecodeModesAndBases(c *C) {
	tt := []struct {
		comment  string
		bases    []string
		modes    []string
		expbases []string
		expmodes []string
		err      string
	}{
		// happy uc20+ cases
		{
			comment:  "core20 base with run mode",
			bases:    []string{"core20"},
			modes:    []string{"run"},
			expbases: []string{"core20"},
			expmodes: []string{"run"},
		},
		{
			comment:  "core20 base with recover mode",
			bases:    []string{"core20"},
			modes:    []string{"recover"},
			expbases: []string{"core20"},
			expmodes: []string{"recover"},
		},
		{
			comment:  "core20 base with recover and run modes",
			bases:    []string{"core20"},
			modes:    []string{"recover", "run"},
			expbases: []string{"core20"},
			expmodes: []string{"recover", "run"},
		},
		{
			comment:  "core22 base with run mode",
			bases:    []string{"core22"},
			modes:    []string{"run"},
			expbases: []string{"core22"},
			expmodes: []string{"run"},
		},
		{
			comment:  "core20 and core22 bases with run mode",
			bases:    []string{"core20", "core22"},
			modes:    []string{"run"},
			expbases: []string{"core20", "core22"},
			expmodes: []string{"run"},
		},
		{
			comment:  "core20 and core22 bases with run and recover modes",
			bases:    []string{"core20", "core22"},
			modes:    []string{"run", "recover"},
			expbases: []string{"core20", "core22"},
			expmodes: []string{"run", "recover"},
		},
		{
			comment:  "all bases with run mode (effectively all uc20 bases)",
			modes:    []string{"run"},
			expmodes: []string{"run"},
		},
		{
			comment:  "all bases with recover mode (effectively all uc20 bases)",
			modes:    []string{"recover"},
			expmodes: []string{"recover"},
		},
		{
			comment:  "core20 base with empty modes",
			bases:    []string{"core20"},
			expbases: []string{"core20"},
		},
		{
			comment:  "core22 base with empty modes",
			bases:    []string{"core22"},
			expbases: []string{"core22"},
		},

		// unhappy uc20 cases
		{
			comment: "core20 base with single invalid mode",
			bases:   []string{"core20"},
			modes:   []string{"not-a-real-uc20-mode"},
			err:     `assertion repair: header \"modes\" contains an invalid element: \"not-a-real-uc20-mode\" \(valid values are run and recover\)`,
		},
		{
			comment: "core20 base with invalid modes",
			bases:   []string{"core20"},
			modes:   []string{"run", "not-a-real-uc20-mode"},
			err:     `assertion repair: header \"modes\" contains an invalid element: \"not-a-real-uc20-mode\" \(valid values are run and recover\)`,
		},
		{
			comment: "core20 base with install mode",
			bases:   []string{"core20"},
			modes:   []string{"install"},
			err:     `assertion repair: header \"modes\" contains an invalid element: \"install\" \(valid values are run and recover\)`,
		},

		// happy uc18/uc16 cases
		{
			comment:  "core18 base with empty modes",
			bases:    []string{"core18"},
			expbases: []string{"core18"},
		},
		{
			comment:  "core base with empty modes",
			bases:    []string{"core"},
			expbases: []string{"core"},
		},

		// unhappy uc18/uc16 cases
		{
			comment: "core18 base with non-empty modes",
			bases:   []string{"core18"},
			modes:   []string{"run"},
			err:     "assertion repair: in the presence of a non-empty \"modes\" header, \"bases\" must only contain base snaps supporting recovery modes",
		},
		{
			comment: "core base with non-empty modes",
			bases:   []string{"core"},
			modes:   []string{"run"},
			err:     "assertion repair: in the presence of a non-empty \"modes\" header, \"bases\" must only contain base snaps supporting recovery modes",
		},
		{
			comment: "core16 base with non-empty modes",
			bases:   []string{"core16"},
			modes:   []string{"run"},
			err:     "assertion repair: in the presence of a non-empty \"modes\" header, \"bases\" must only contain base snaps supporting recovery modes",
		},

		// unhappy non-core specific cases
		{
			comment: "invalid snap name as base",
			bases:   []string{"foo....bar"},
			err:     "assertion repair: invalid snap name \"foo....bar\" in \"bases\"",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		repairStr := strings.Replace(repairExample, "MODELSLINE", s.modelsLine, 1)
		repairStr = strings.Replace(repairStr, "TSLINE", s.tsLine, 1)

		var basesStr, modesStr string
		if len(t.bases) != 0 {
			basesStr = "bases:\n"
			for _, b := range t.bases {
				basesStr += "  - " + b + "\n"
			}
		}
		if len(t.modes) != 0 {
			modesStr = "modes:\n"
			for _, m := range t.modes {
				modesStr += "  - " + m + "\n"
			}
		}

		repairStr = strings.Replace(repairStr, "BASESLINE", basesStr, 1)
		repairStr = strings.Replace(repairStr, "MODESLINE", modesStr, 1)

		assert := mylog.Check2(asserts.Decode([]byte(repairStr)))
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
		} else {
			c.Assert(err, IsNil, comment)
			repair, ok := assert.(*asserts.Repair)
			c.Assert(ok, Equals, true, comment)

			c.Assert(repair.Bases(), DeepEquals, t.expbases, comment)
			c.Assert(repair.Modes(), DeepEquals, t.expmodes, comment)
		}
	}
}

func (s *repairSuite) TestDecodeInvalid(c *C) {
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series:\n  - 16\n", "series: \n", `"series" header must be a list of strings`},
		{"series:\n  - 16\n", "series: something\n", `"series" header must be a list of strings`},
		{"architectures:\n  - amd64\n  - arm64\n", "architectures: foo\n", `"architectures" header must be a list of strings`},
		{"models:\n  - acme/frobinator\n", "models: \n", `"models" header must be a list of strings`},
		{"models:\n  - acme/frobinator\n", "models: something\n", `"models" header must be a list of strings`},
		{"repair-id: 42\n", "repair-id: no-number\n", `"repair-id" header is not an integer: no-number`},
		{"repair-id: 42\n", "repair-id: 0\n", `"repair-id" must be >=1: 0`},
		{"repair-id: 42\n", "repair-id: 01\n", `"repair-id" header has invalid prefix zeros: 01`},
		{"repair-id: 42\n", "repair-id: 99999999999999999999\n", `"repair-id" header is out of range: 99999999999999999999`},
		{"brand-id: acme\n", "brand-id: brand-id-not-eq-authority-id\n", `authority-id and brand-id must match, repair assertions are expected to be signed by the brand: "acme" != "brand-id-not-eq-authority-id"`},
		{"summary: example repair\n", "", `"summary" header is mandatory`},
		{"summary: example repair\n", "summary: \n", `"summary" header should not be empty`},
		{"summary: example repair\n", "summary:\n    multi\n    line\n", `"summary" header cannot have newlines`},
		{s.tsLine, "", `"timestamp" header is mandatory`},
		{s.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{s.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(s.repairStr, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, repairErrPrefix+test.expectedErr)
	}
}

// FIXME: move to a different layer later
func (s *repairSuite) TestRepairCanEmbedScripts(c *C) {
	a := mylog.Check2(asserts.Decode([]byte(s.repairStr)))

	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)

	tmpdir := c.MkDir()
	repairScript := filepath.Join(tmpdir, "repair")
	mylog.Check(os.WriteFile(repairScript, []byte(repair.Body()), 0755))

	cmd := exec.Command(repairScript)
	cmd.Dir = tmpdir
	output := mylog.Check2(cmd.CombinedOutput())
	c.Check(err, IsNil)
	c.Check(string(output), Equals, `Unpack embedded payload
hello from the inside
`)
}
