package notify_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type notifySuite struct {
	testutil.BaseTest
}

var _ = Suite(&notifySuite{})

func (s *notifySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (*notifySuite) TestSysPathBehavior(c *C) {
	newRoot := c.MkDir()
	newSysPath := filepath.Join(newRoot, "/sys/kernel/security/apparmor/.notify")
	dirs.SetRootDir(newRoot)
	c.Assert(notify.SysPath, Equals, newSysPath)
}

func (*notifySuite) TestSupportAvailable(c *C) {
	newRoot := c.MkDir()
	dirs.SetRootDir(newRoot)
	c.Assert(notify.SupportAvailable(), Equals, false)
	mylog.Check(os.MkdirAll(filepath.Dir(notify.SysPath), 0755))

	c.Assert(notify.SupportAvailable(), Equals, false)
	_ = mylog.Check2(os.Create(notify.SysPath))

	c.Assert(notify.SupportAvailable(), Equals, true)
}
