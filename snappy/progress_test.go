package snappy

import (
	"io/ioutil"
	"os"

	"fmt"
	. "launchpad.net/gocheck"
)

type ProgressTestSuite struct{}

var _ = Suite(&ProgressTestSuite{})

func (ts *ProgressTestSuite) TestSpin(c *C) {
	f, err := ioutil.TempFile("", "progress-")
	c.Assert(err, IsNil)
	defer os.Remove(f.Name())
	oldStdout := os.Stdout
	os.Stdout = f

	t := NewTextProgress("no-pkg")
	for i := 0; i < 6; i++ {
		t.Spin("m")
	}

	os.Stdout = oldStdout
	f.Sync()
	f.Seek(0, 0)
	progress, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(progress), Equals, "\rm[|]\rm[/]\rm[-]\rm[\\]\rm[|]\rm[/]")
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

	t := NewTextProgress("no-pkg")
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
