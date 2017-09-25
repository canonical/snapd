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
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/progress/progresstest"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ProgressTestSuite struct {
}

var _ = Suite(&ProgressTestSuite{})

func (ts *ProgressTestSuite) TestSpin(c *C) {
	f, err := ioutil.TempFile("", "progress-")
	c.Assert(err, IsNil)
	defer os.Remove(f.Name())
	oldStdout := os.Stdout
	os.Stdout = f

	progress.MockEmptyEscapes()

	t := &progress.ANSIMeter{}
	for i := 0; i < 6; i++ {
		t.Spin("msg")
	}

	os.Stdout = oldStdout
	f.Sync()
	f.Seek(0, 0)
	progress, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(progress), Equals, strings.Repeat(fmt.Sprintf("\r%-80s", "msg"), 6))
}

func (ts *ProgressTestSuite) testNotify(c *C, t progress.Meter, desc, expected string) {
	oldStdout := os.Stdout
	defer func() {
		os.Stdout = oldStdout
	}()

	comment := Commentf(desc)

	fout, err := ioutil.TempFile("", "notify-out-")
	c.Assert(err, IsNil)
	defer fout.Close()
	os.Stdout = fout

	t.Notify("blah blah")

	_, err = fout.Seek(0, 0)
	c.Assert(err, IsNil, comment)

	out, err := ioutil.ReadAll(fout)
	c.Assert(err, IsNil, comment)
	c.Check(string(out), Equals, expected, comment)
}

func (ts *ProgressTestSuite) TestQuietNotify(c *C) {
	ts.testNotify(c, &progress.QuietMeter{}, "quiet", "blah blah\n")
}

func (ts *ProgressTestSuite) TestANSINotify(c *C) {
	expected := fmt.Sprint("\r", progress.ExitAttributeMode, progress.ClrEOL, "blah blah\n")
	ts.testNotify(c, &progress.ANSIMeter{}, "ansi", expected)
}

func (ts *ProgressTestSuite) TestTestingNotify(c *C) {
	ts.testNotify(c, &progresstest.Meter{}, "test", "")
}
