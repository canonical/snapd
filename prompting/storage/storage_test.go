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
		Path:       "/home/test/foo/",
	}

	allowed, err := st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Check(allowed, Equals, false)

	allow := true
	extra := map[string]string{}
	extra["allow-subdirectories"] = "yes"
	extra["deny-subdirectories"] = "yes"
	err = st.Set(req, allow, extra)
	c.Assert(err, IsNil)

	paths := st.MapsForUidAndLabel(1000, "snap.lxd.lxd").AllowWithSubdirs
	c.Check(paths, HasLen, 1)

	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)

	// set a more nested path
	req.Path = "/home/test/foo/bar"
	err = st.Set(req, allow, extra)
	c.Assert(err, IsNil)
	// more nested path is not added
	c.Check(paths, HasLen, 1)

	// and more nested path is still allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)
}

func (s *storageSuite) TestSubdirOverrides(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo/",
	}

	allowed, err := st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Check(allowed, Equals, false)

	allow := true
	extra := map[string]string{}
	extra["allow-subdirectories"] = "yes"
	extra["deny-subdirectories"] = "yes"
	err = st.Set(req, allow, extra)
	c.Assert(err, IsNil)

	// set a more nested path to not allow
	req.Path = "/home/test/foo/bar/"
	err = st.Set(req, !allow, extra)
	c.Assert(err, IsNil)
	// more nested path was added
	paths := st.MapsForUidAndLabel(1000, "snap.lxd.lxd").AllowWithSubdirs
	c.Check(paths, HasLen, 2)

	// and check more nested path is not allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, false)
	// and even more nested path is not allowed
	req.Path = "/home/test/foo/bar/baz"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, false)
	// but original path is still allowed
	req.Path = "/home/test/foo/"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)

	// set original path to not allow
	err = st.Set(req, !allow, extra)
	c.Assert(err, IsNil)
	// original assignment was overridden
	c.Check(paths, HasLen, 2)
	// TODO: in the future, possibly this should be 1, if we decide to remove
	// subdirectories with the same access from the database when an ancestor
	// directory with the same access is added

	// set a more nested path to allow
	req.Path = "/home/test/foo/bar/"
	err = st.Set(req, allow, extra)
	c.Assert(err, IsNil)
	// more nested path was added
	c.Check(paths, HasLen, 2)

	// and check more nested path is allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)
	// and even more nested path is also allowed
	req.Path = "/home/test/foo/bar/baz"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, true)
	// but original path is still not allowed
	req.Path = "/home/test/foo/"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Check(allowed, Equals, false)
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
