package prompting_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/apparmor/prompting"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type promptingSuite struct {
	testutil.BaseTest
}

var _ = Suite(&promptingSuite{})

func (s *promptingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (*promptingSuite) TestNotifyPathBehavior(c *C) {
	newRoot := c.MkDir()
	newNotifyPath := filepath.Join(newRoot, "/sys/kernel/security/apparmor/.notify")
	dirs.SetRootDir(newRoot)
	c.Assert(prompting.NotifyPath, Equals, newNotifyPath)
}

func (*promptingSuite) TestPromptingSupportAvailable(c *C) {
	newRoot := c.MkDir()
	dirs.SetRootDir(newRoot)
	c.Assert(prompting.PromptingSupportAvailable(), Equals, false)
	err := os.MkdirAll(filepath.Dir(prompting.NotifyPath), 0755)
	c.Assert(err, IsNil)
	c.Assert(prompting.PromptingSupportAvailable(), Equals, false)
	_, err = os.Create(prompting.NotifyPath)
	c.Assert(err, IsNil)
	c.Assert(prompting.PromptingSupportAvailable(), Equals, true)
}
