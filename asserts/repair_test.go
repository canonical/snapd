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

const repairExample = "type: repair\n" +
	"authority-id: canonical\n" +
	"repair-id: REPAIR-42\n" +
	"series:\n" +
	"  - 16\n" +
	"MODELSLINE\n" +
	"cmd: sed 's/broked/fixed/s' /etc/systemd/system/snapd.service\n" +
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
}

func (em *repairSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(em.repairStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.RepairType)
	repair := a.(*asserts.Repair)
	c.Check(repair.RepairID(), Equals, "REPAIR-42")
	c.Check(repair.Series(), DeepEquals, []string{"16"})
	c.Check(repair.Models(), DeepEquals, []string{"frobinator"})
	c.Check(repair.Cmd(), Equals, "sed 's/broked/fixed/s' /etc/systemd/system/snapd.service")
	c.Check(repair.Since().Equal(em.since), Equals, true)
	c.Check(repair.Until().Equal(em.until), Equals, true)
}
