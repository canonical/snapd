package snappy

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
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
