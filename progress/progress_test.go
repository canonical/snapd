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

package progress

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ProgressTestSuite struct {
	attachedToTerminalReturn bool

	originalAttachedToTerminal func() bool
}

var _ = Suite(&ProgressTestSuite{})

func (ts *ProgressTestSuite) MockAttachedToTerminal() bool {
	return ts.attachedToTerminalReturn
}

func (ts *ProgressTestSuite) TestSpin(c *C) {
	f, err := ioutil.TempFile("", "progress-")
	c.Assert(err, IsNil)
	defer os.Remove(f.Name())
	oldStdout := os.Stdout
	os.Stdout = f

	t := NewTextProgress()
	for i := 0; i < 6; i++ {
		t.Spin("msg")
	}

	os.Stdout = oldStdout
	f.Sync()
	f.Seek(0, 0)
	progress, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(progress), Equals, "\r[|] msg\x1b[K\r[/] msg\x1b[K\r[-] msg\x1b[K\r[\\] msg\x1b[K\r[|] msg\x1b[K\r[/] msg\x1b[K")
}

func (ts *ProgressTestSuite) testAgreed(answer string, value bool, c *C) {
	fout, err := ioutil.TempFile("", "progress-out-")
	c.Assert(err, IsNil)
	oldStdout := os.Stdout
	os.Stdout = fout
	defer func() {
		os.Stdout = oldStdout
		os.Remove(fout.Name())
		fout.Close()
	}()

	fin, err := ioutil.TempFile("", "progress-in-")
	c.Assert(err, IsNil)
	oldStdin := os.Stdin
	os.Stdin = fin
	defer func() {
		os.Stdin = oldStdin
		os.Remove(fin.Name())
		fin.Close()
	}()

	_, err = fmt.Fprintln(fin, answer)
	c.Assert(err, IsNil)
	_, err = fin.Seek(0, 0)
	c.Assert(err, IsNil)

	license := "Void where empty."

	t := NewTextProgress()
	c.Check(t.Agreed("blah blah", license), Equals, value)

	_, err = fout.Seek(0, 0)
	c.Assert(err, IsNil)
	out, err := ioutil.ReadAll(fout)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "blah blah\n"+license+"\nDo you agree? [y/n] ")
}

func (ts *ProgressTestSuite) TestAgreed(c *C) {
	ts.testAgreed("Y", true, c)
	ts.testAgreed("N", false, c)
}

func (ts *ProgressTestSuite) TestNotify(c *C) {
	fout, err := ioutil.TempFile("", "notify-out-")
	c.Assert(err, IsNil)
	oldStdout := os.Stdout
	os.Stdout = fout
	defer func() {
		os.Stdout = oldStdout
		os.Remove(fout.Name())
		fout.Close()
	}()

	t := NewTextProgress()
	t.Notify("blah blah")

	_, err = fout.Seek(0, 0)
	c.Assert(err, IsNil)
	out, err := ioutil.ReadAll(fout)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "blah blah\n")
}

func (ts *ProgressTestSuite) TestMakeProgressBar(c *C) {
	var pbar Meter

	ts.originalAttachedToTerminal = attachedToTerminal
	attachedToTerminal = ts.MockAttachedToTerminal
	defer func() {
		// reset
		attachedToTerminal = ts.originalAttachedToTerminal
	}()

	ts.attachedToTerminalReturn = true

	pbar = MakeProgressBar()
	c.Assert(pbar, FitsTypeOf, NewTextProgress())

	ts.attachedToTerminalReturn = false

	pbar = MakeProgressBar()
	c.Assert(pbar, FitsTypeOf, &NullProgress{})

}
