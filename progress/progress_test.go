// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package progress_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/progress/progresstest"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ProgressTestSuite struct{}

var _ = Suite(&ProgressTestSuite{})

func (ts *ProgressTestSuite) TestMakeProgressBar(c *C) {
	defer progress.MockIsTerminal(false)()

	m := progress.MakeProgressBar(nil)
	c.Check(m, FitsTypeOf, progress.QuietMeter{})
	m = progress.MakeProgressBar(os.Stdout)
	c.Check(m, FitsTypeOf, progress.QuietMeter{})

	progress.MockIsTerminal(true)
	m = progress.MakeProgressBar(nil)
	c.Check(m, FitsTypeOf, &progress.ANSIMeter{})
	m = progress.MakeProgressBar(os.Stdout)
	c.Check(m, FitsTypeOf, &progress.ANSIMeter{})
	var buf bytes.Buffer
	m = progress.MakeProgressBar(&buf)
	c.Check(m, FitsTypeOf, progress.QuietMeter{})
}

func (ts *ProgressTestSuite) testNotify(c *C, t progress.Meter, desc, expected string) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()

	comment := Commentf(desc)

	t.Notify("blah blah")

	c.Check(buf.String(), Equals, expected, comment)
}

func (ts *ProgressTestSuite) TestQuietNotify(c *C) {
	ts.testNotify(c, &progress.QuietMeter{}, "quiet", "blah blah\n")
}

func (ts *ProgressTestSuite) TestOtherWriterQuietNotify(c *C) {
	var buf bytes.Buffer

	m := progress.MakeProgressBar(&buf)
	ts.testNotify(c, m, "quiet", "")

	c.Check(buf.String(), Equals, "blah blah\n")
}

func (ts *ProgressTestSuite) TestANSINotify(c *C) {
	expected := fmt.Sprint("\r", progress.ExitAttributeMode, progress.ClrEOL, "blah blah\n")
	ts.testNotify(c, &progress.ANSIMeter{}, "ansi", expected)
}

func (ts *ProgressTestSuite) TestTestingNotify(c *C) {
	p := &progresstest.Meter{}
	ts.testNotify(c, p, "test", "")
	c.Check(p.Notices, DeepEquals, []string{"blah blah"})
}
