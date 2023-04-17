package storage_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/prompting/notifier"
	"github.com/snapcore/snapd/prompting/storage"
)

func Test(t *testing.T) { TestingT(t) }

type storageSuite struct {
	tmpdir string
}

var _ = Suite(&storageSuite{})

func (s *storageSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *storageSuite) TestSimple(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo",
	}

	allowed, err := st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Check(allowed, Equals, false)

	allow := true
	extra := map[string]string{}
	err = st.Set(req, allow, extra)
	c.Assert(err, IsNil)

	paths := st.PathsForUidAndLabel(1000, "snap.lxd.lxd")
	c.Check(paths, HasLen, 1)

	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)

	// set a more nested path
	req.Path = "/home/test/foo/bar"
	err = st.Set(req, allow, extra)
	c.Assert(err, IsNil)
	// more nested path is not added
	paths = st.PathsForUidAndLabel(1000, "snap.lxd.lxd")
	c.Check(paths, HasLen, 1)

	// and more nested path is still allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)
}

func (s *storageSuite) TestLoadSave(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo",
	}
	allow := true
	err := st.Set(req, allow, nil)
	c.Assert(err, IsNil)

	// st2 will read DB from the previous storage
	st2 := storage.New()
	allowed, err := st2.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)
}
