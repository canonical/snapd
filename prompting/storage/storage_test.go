package storage_test

import (
	"reflect"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/prompting/apparmor"
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
		Permission: apparmor.MayReadPermission,
	}

	allowed, err := st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	allow := true
	extras := map[storage.ExtrasKey]string{}
	extras[storage.ExtrasAllowWithSubdirs] = "yes"
	extras[storage.ExtrasDenyWithSubdirs] = "yes"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)

	paths := st.MapsForUidAndLabelAndPermission(1000, "snap.lxd.lxd", "read").AllowWithSubdirs
	c.Assert(paths, HasLen, 1)

	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)

	// set a more nested path
	req.Path = "/home/test/foo/bar"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)
	// more nested path is not added
	c.Assert(paths, HasLen, 1)

	// and more nested path is still allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
}

func (s *storageSuite) TestSubdirOverrides(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo/",
		Permission: apparmor.MayReadPermission,
	}

	allowed, err := st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	allow := true
	extras := map[storage.ExtrasKey]string{}
	extras[storage.ExtrasAllowWithSubdirs] = "yes"
	extras[storage.ExtrasDenyWithSubdirs] = "yes"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)

	// set a more nested path to not allow
	req.Path = "/home/test/foo/bar/"
	_, err = st.Set(req, !allow, extras)
	c.Assert(err, IsNil)
	// more nested path was added
	paths := st.MapsForUidAndLabelAndPermission(1000, "snap.lxd.lxd", "read").AllowWithSubdirs
	c.Assert(paths, HasLen, 2)

	// and check more nested path is not allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)
	// and even more nested path is not allowed
	req.Path = "/home/test/foo/bar/baz"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)
	// but original path is still allowed
	req.Path = "/home/test/foo/"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)

	// set original path to not allow
	_, err = st.Set(req, !allow, extras)
	c.Assert(err, IsNil)
	// original assignment was overridden, and older more specific decisions
	// were removed
	c.Assert(paths, HasLen, 1)
	// TODO: in the future, possibly this should be 1, if we decide to remove
	// subdirectories with the same access from the database when an ancestor
	// directory with the same access is added

	// set a more nested path to allow
	req.Path = "/home/test/foo/bar/"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)
	// more nested path was added
	c.Assert(paths, HasLen, 2)

	// and check more nested path is allowed
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
	// and even more nested path is also allowed
	req.Path = "/home/test/foo/bar/baz"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
	// but original path is still not allowed
	req.Path = "/home/test/foo/"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)
}

func cloneAllowMap(m map[string]bool) map[string]bool {
	newMap := make(map[string]bool, len(m))
	for k, v := range m {
		newMap[k] = v
	}
	return newMap
}

func (s *storageSuite) TestGetMatches(c *C) {
	cases := []struct {
		allow            map[string]bool
		allowWithDir     map[string]bool
		allowWithSubdirs map[string]bool
		path             string
		decision         bool
	}{
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/bar",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo/bar",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo/bar/baz",
			true,
		},
		{
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo",
			false,
		},
		{
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{"/home/test": false},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{"/home/test": false},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/bar",
			true,
		},
		{
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar/baz",
			true,
		},
		{
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar",
			true,
		},
		{
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo",
			false,
		},
		{
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test/foo/bar/baz",
			false,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test/foo/bar",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test",
			false,
		},
		{
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test/foo/bar/baz",
			false,
		},
		{
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test/foo/bar",
			false,
		},
		{
			map[string]bool{"/home/test/foo/bar": true},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar",
			true,
		},
		{
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test/foo",
			true,
		},
		{
			map[string]bool{"/home/test/foo/bar": true},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			"/home/test/foo",
			false,
		},
		{
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
			"/home/test",
			false,
		},
		{
			map[string]bool{"/home/test/foo/bar": true},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			"/home/test",
			true,
		},
	}

	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "placeholder",
		Permission: apparmor.MayReadPermission,
	}

	permissionEntries := st.MapsForUidAndLabelAndPermission(req.SubjectUid, req.Label, "read")

	for i, testCase := range cases {
		permissionEntries.Allow = cloneAllowMap(testCase.allow)
		permissionEntries.AllowWithDir = cloneAllowMap(testCase.allowWithDir)
		permissionEntries.AllowWithSubdirs = cloneAllowMap(testCase.allowWithSubdirs)
		req.Path = testCase.path
		allow, err := st.Get(req)
		c.Check(err, IsNil, Commentf("\nTest case %d:\n%+v\nError: %v", i, testCase, err))
		c.Check(allow, Equals, testCase.decision, Commentf("\nTest case %d:\n%+v\nGet() returned: %v", i, testCase, allow))
	}
}

func (s *storageSuite) TestGetErrors(c *C) {
	cases := []struct {
		allow            map[string]bool
		allowWithDir     map[string]bool
		allowWithSubdirs map[string]bool
		path             string
		err              error
	}{
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/bar",
			storage.ErrNoSavedDecision,
		},
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test",
			storage.ErrNoSavedDecision,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/bar/baz",
			storage.ErrNoSavedDecision,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test",
			storage.ErrNoSavedDecision,
		},
		{
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test",
			storage.ErrNoSavedDecision,
		},
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo",
			storage.ErrMultipleDecisions,
		},
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			storage.ErrMultipleDecisions,
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			storage.ErrMultipleDecisions,
		},
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			storage.ErrMultipleDecisions,
		},
	}

	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "placeholder",
		Permission: apparmor.MayReadPermission,
	}

	permissionEntries := st.MapsForUidAndLabelAndPermission(req.SubjectUid, req.Label, "read")

	for i, testCase := range cases {
		permissionEntries.Allow = cloneAllowMap(testCase.allow)
		permissionEntries.AllowWithDir = cloneAllowMap(testCase.allowWithDir)
		permissionEntries.AllowWithSubdirs = cloneAllowMap(testCase.allowWithSubdirs)
		req.Path = testCase.path
		_, err := st.Get(req)
		c.Check(err, Equals, testCase.err, Commentf("\nTest case %d:\n%+v\nUnexpected Error: %v", i, testCase, err))
	}
}

func (s *storageSuite) TestSetBehaviorWithMatches(c *C) {
	// Test that Set() adds new decisions correctly if there are existing
	// decisions which match the new decision path

	// if path matches entry already in a different map (XXX means can't return early):
	// new Allow, old Allow -> replace if different
	// new Allow, old AllowWithDir, exact match -> replace if different (forces prompt for entries in directory of path)
	// new Allow, old AllowWithSubdirs, exact match -> same as ^^
	// new Allow, old AllowWithDir, parent match -> insert if different
	// new Allow, old AllowWithSubdirs, ancestor match -> same as ^^
	// new AllowWithDir, old Allow -> replace always XXX
	// new AllowWithDir, old AllowWithDir, exact match -> replace if different
	// new AllowWithDir, old AllowWithSubdirs, exact match -> same as ^^
	// new AllowWithDir, old AllowWithDir, parent match -> insert always XXX
	// new AllowWithDir, old AllowWithSubdirs, ancestor match -> insert if different
	// new AllowWithSubdirs, old Allow -> replace always XXX
	// new AllowWithSubdirs, old AllowWithDir, exact match -> replace always XXX
	// new AllowWithSubdirs, old AllowWithSubdirs, exact match -> replace if different
	// new AllowWithSubdirs, old AllowWithDir, parent match -> insert always XXX
	// new AllowWithSubdirs, old AllowWithSubdirs, ancestor match -> insert if different
	cases := []struct {
		initialAllow            map[string]bool
		initialAllowWithDir     map[string]bool
		initialAllowWithSubdirs map[string]bool
		path                    string
		decision                bool
		extras                  map[storage.ExtrasKey]string
		finalAllow              map[string]bool
		finalAllowWithDir       map[string]bool
		finalAllowWithSubdirs   map[string]bool
	}{
		{ // new Allow, old Allow -> replace if different
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
		},
		{ // new Allow, old Allow -> replace if different
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithDir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithDir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithDir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithSubdirs, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new Allow, old AllowWithSubdirs, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithSubdirs, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithDir, parent match -> insert if different
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithDir, parent match -> insert if different
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithDir, no match -> insert always
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo/bar",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo/bar": true},
			map[string]bool{"/home/test": true},
			map[string]bool{},
		},
		{ // new Allow, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
		},
		{ // new Allow, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{},
			map[string]bool{"/home/test": true},
		},
		{ // new Allow, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": false},
			"/home/test/foo/bar",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo/bar": true},
			map[string]bool{},
			map[string]bool{"/home/test": false},
		},
		{ // new Allow, old AllowWithSubdirs, no match -> insert always
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/bar",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/bar": true},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new Allow, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/bar",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/bar": false},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new AllowWithDir, old Allow -> replace always
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
		},
		{ // new AllowWithDir, old Allow -> replace always
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
		},
		{ // new AllowWithDir, old Allow -> replace always
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithDir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithDir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithSubdir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new AllowWithDir, old AllowWithSubdir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithSubdir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithSubdir, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithDir, old AllowWithDir, parent match -> insert always
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test": true, "/home/test/foo": true},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithDir, parent match -> insert always
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test": true, "/home/test/foo": false},
			map[string]bool{},
		},
		{ // new AllowWithDir, old AllowWithSubdir, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
		},
		{ // new AllowWithDir, old AllowWithSubdir, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo/bar": false},
			map[string]bool{"/home/test": true},
		},
		{ // new AllowWithDir, old AllowWithSubdir, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": false},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{"/home/test": false},
		},
		{ // new AllowWithSubdirs, old Allow -> replace always
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new AllowWithSubdirs, old Allow -> replace always
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old Allow -> replace always
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, exact match -> replace always
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, exact match -> replace always
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, exact match -> replace always
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, exact match -> replace always
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old AllowWithSubdirs, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new AllowWithSubdirs, old AllowWithSubdirs, exact match -> replace if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, parent match -> insert always
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{"/home/test/foo": true},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, parent match -> insert always
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{"/home/test/foo": false},
		},
		{ // new AllowWithSubdirs, old AllowWithDir, no match -> insert always
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{},
			"/home/test/foo/bar/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			map[string]bool{"/home/test/foo/bar": true},
		},
		{ // new AllowWithSubdirs, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
		},
		{ // new AllowWithSubdirs, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
			"/home/test/foo/bar/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true, "/home/test/foo/bar": false},
		},
		{ // new AllowWithSubdirs, old AllowWithSubdirs, ancestor match -> insert if different
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": false},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": false},
		},
		{ // new AllowWithSubdirs, old AllowWithSubdirs, no match -> insert always
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/bar/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true, "/home/test/bar": true},
		},
	}

	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "placeholder",
		Permission: apparmor.MayReadPermission,
	}

	permissionEntries := st.MapsForUidAndLabelAndPermission(req.SubjectUid, req.Label, "read")

	for i, testCase := range cases {
		permissionEntries.Allow = cloneAllowMap(testCase.initialAllow)
		permissionEntries.AllowWithDir = cloneAllowMap(testCase.initialAllowWithDir)
		permissionEntries.AllowWithSubdirs = cloneAllowMap(testCase.initialAllowWithSubdirs)
		req.Path = testCase.path
		_, result := st.Set(req, testCase.decision, testCase.extras)
		c.Check(reflect.DeepEqual(permissionEntries.Allow, testCase.finalAllow), Equals, true, Commentf("\nTest case %d:\n%+v\nAllow does not match\nActual Allow: %+v\nActual AllowWithDir: %+v\nActualAllowWithSubdirs: %+v\nSet() returned: %v\n", i, testCase, permissionEntries.Allow, permissionEntries.AllowWithDir, permissionEntries.AllowWithSubdirs, result))
		c.Check(reflect.DeepEqual(permissionEntries.AllowWithDir, testCase.finalAllowWithDir), Equals, true, Commentf("\nTest case %d:\n%+v\nAllowWithDir does not match\nActual Allow: %+v\nActual AllowWithDir: %+v\nActualAllowWithSubdirs: %+v\nSet() returned: %v\n", i, testCase, permissionEntries.Allow, permissionEntries.AllowWithDir, permissionEntries.AllowWithSubdirs, result))
		c.Check(reflect.DeepEqual(permissionEntries.AllowWithSubdirs, testCase.finalAllowWithSubdirs), Equals, true, Commentf("\nTest case %d:\n%+v\nAllowWithSubdirs does not match\nActual Allow: %+v\nActual AllowWithDir: %+v\nActualAllowWithSubdirs: %+v\nSet() returned: %v\n", i, testCase, permissionEntries.Allow, permissionEntries.AllowWithDir, permissionEntries.AllowWithSubdirs, result))
	}
}

func (s *storageSuite) TestSetDecisionPruning(c *C) {
	// Test that Set() removes old decisions correctly if they are more
	// specific than the new rule and should thus be overwritten

	cases := []struct {
		initialAllow            map[string]bool
		initialAllowWithDir     map[string]bool
		initialAllowWithSubdirs map[string]bool
		path                    string
		decision                bool
		extras                  map[storage.ExtrasKey]string
		finalAllow              map[string]bool
		finalAllowWithDir       map[string]bool
		finalAllowWithSubdirs   map[string]bool
	}{
		{
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			map[string]bool{},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
		},
		{
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			map[string]bool{},
		},
		{
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": true},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{},
			map[string]bool{},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			"/home/test/foo",
			false,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo/bar.txt": true},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true},
			map[string]bool{"/home/test/foo": false},
			map[string]bool{"/home/test": true},
			"/home/test/foo",
			true,
			map[storage.ExtrasKey]string{},
			map[string]bool{"/home/test/foo/bar.txt": true},
			map[string]bool{},
			map[string]bool{"/home/test": true},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/subdir": false},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithDir: "yes"},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false, "/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo/subdir": false},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/subdir": false},
			"/home/test/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithDir: "yes"},
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test": true, "/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/subdir": false},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/subdir": false},
			"/home/test/",
			true,
			map[storage.ExtrasKey]string{storage.ExtrasAllowWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test": true},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/subdir": false},
			"/home/test/foo/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{},
			map[string]bool{},
			map[string]bool{"/home/test/foo": false},
		},
		{
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test/foo/dir1": true, "/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/subdir": false},
			"/home/test/foo/dir1/",
			false,
			map[storage.ExtrasKey]string{storage.ExtrasDenyWithSubdirs: "yes"},
			map[string]bool{"/home/test/foo/bar.txt": true, "/home/test/foo/baz.txt": false},
			map[string]bool{"/home/test/foo/dir2": false},
			map[string]bool{"/home/test/foo": true, "/home/test/foo/dir1": false, "/home/test/foo/subdir": false},
		},
	}

	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "placeholder",
		Permission: apparmor.MayReadPermission,
	}

	permissionEntries := st.MapsForUidAndLabelAndPermission(req.SubjectUid, req.Label, "read")

	for i, testCase := range cases {
		permissionEntries.Allow = cloneAllowMap(testCase.initialAllow)
		permissionEntries.AllowWithDir = cloneAllowMap(testCase.initialAllowWithDir)
		permissionEntries.AllowWithSubdirs = cloneAllowMap(testCase.initialAllowWithSubdirs)
		req.Path = testCase.path
		_, result := st.Set(req, testCase.decision, testCase.extras)
		c.Check(reflect.DeepEqual(permissionEntries.Allow, testCase.finalAllow), Equals, true, Commentf("\nTest case %d:\n%+v\nAllow does not match\nActual Allow: %+v\nActual AllowWithDir: %+v\nActualAllowWithSubdirs: %+v\nSet() returned: %v\n", i, testCase, permissionEntries.Allow, permissionEntries.AllowWithDir, permissionEntries.AllowWithSubdirs, result))
		c.Check(reflect.DeepEqual(permissionEntries.AllowWithDir, testCase.finalAllowWithDir), Equals, true, Commentf("\nTest case %d:\n%+v\nAllowWithDir does not match\nActual Allow: %+v\nActual AllowWithDir: %+v\nActualAllowWithSubdirs: %+v\nSet() returned: %v\n", i, testCase, permissionEntries.Allow, permissionEntries.AllowWithDir, permissionEntries.AllowWithSubdirs, result))
		c.Check(reflect.DeepEqual(permissionEntries.AllowWithSubdirs, testCase.finalAllowWithSubdirs), Equals, true, Commentf("\nTest case %d:\n%+v\nAllowWithSubdirs does not match\nActual Allow: %+v\nActual AllowWithDir: %+v\nActualAllowWithSubdirs: %+v\nSet() returned: %v\n", i, testCase, permissionEntries.Allow, permissionEntries.AllowWithDir, permissionEntries.AllowWithSubdirs, result))
	}
}

func (s *storageSuite) TestFindChildrenInMap(c *C) {
	cases := []struct {
		origMap map[string]bool
		path    string
		matches map[string]bool
	}{
		{
			map[string]bool{"/home/test/foo": true, "/home/test/bar/baz.txt": false},
			"/home/test",
			map[string]bool{"/home/test/foo": true},
		},
		{
			map[string]bool{"/home/test/foo": true, "/home/test/bar/baz.txt": false},
			"/home/test/bar",
			map[string]bool{"/home/test/bar/baz.txt": false},
		},
		{
			map[string]bool{"/home/test": true, "/home/test/foo/file.txt": true, "/home/test/bar/baz.txt": false},
			"/home/test/foo",
			map[string]bool{"/home/test/foo/file.txt": true},
		},
		{
			// don't match exact path, only children
			map[string]bool{"/home/test": true, "/home/test/foo": false, "/home/test/foo/file.txt": true, "/home/test/bar": false},
			"/home/test",
			map[string]bool{"/home/test/foo": false, "/home/test/bar": false},
		},
	}
	for i, testCase := range cases {
		actualMatches := storage.FindChildrenInMap(testCase.path, testCase.origMap)
		c.Check(reflect.DeepEqual(testCase.matches, actualMatches), Equals, true, Commentf("\nTest case %d:\n%+v\nIncorrect matches found\nActual matches: %+v", i, testCase, actualMatches))
	}
}

func (s *storageSuite) TestFindDescendantsInMap(c *C) {
	cases := []struct {
		origMap map[string]bool
		path    string
		matches map[string]bool
	}{
		{
			map[string]bool{"/home/test/foo": true, "/home/test/bar/baz.txt": false},
			"/home/test",
			map[string]bool{"/home/test/foo": true, "/home/test/bar/baz.txt": false},
		},
		{
			map[string]bool{"/home/test/foo": true, "/home/test/bar/baz.txt": false},
			"/home/test/bar",
			map[string]bool{"/home/test/bar/baz.txt": false},
		},
		{
			map[string]bool{"/home/test": true, "/home/test/foo/file.txt": true, "/home/test/bar/baz.txt": false},
			"/home/test/foo",
			map[string]bool{"/home/test/foo/file.txt": true},
		},
		{
			// don't match exact path, only descendants
			map[string]bool{"/home/test": true, "/home/test/foo/file.txt": true, "/home/test/bar/baz.txt": false},
			"/home/test",
			map[string]bool{"/home/test/foo/file.txt": true, "/home/test/bar/baz.txt": false},
		},
	}
	for i, testCase := range cases {
		actualMatches := storage.FindDescendantsInMap(testCase.path, testCase.origMap)
		c.Check(reflect.DeepEqual(testCase.matches, actualMatches), Equals, true, Commentf("\nTest case %d:\n%+v\nIncorrect matches found\nActual matches: %+v", i, testCase, actualMatches))
	}
}

func (s *storageSuite) TestPermissionsSimple(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo/",
		Permission: apparmor.MayReadPermission,
	}

	allowed, err := st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	allow := true
	extras := map[storage.ExtrasKey]string{}
	extras[storage.ExtrasAllowWithSubdirs] = "yes"
	extras[storage.ExtrasDenyWithSubdirs] = "yes"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)

	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	// check a different permission
	req.Permission = apparmor.MayWritePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	// set that permission to false with the same path
	allow = false
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)

	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	// check that original permission is still allowed
	req.Permission = apparmor.MayReadPermission
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)

	// set denial of subpath for multiple permissions
	req.Permission = apparmor.MayExecutePermission
	req.Path = "/home/test/foo/bar/"
	allow = false
	extras[storage.ExtrasDenyExtraPerms] = "read,write"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, IsNil)

	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)

	// check that read permission is now false for subpath
	req.Permission = apparmor.MayReadPermission
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, false)
	// but original path is still allowed
	req.Path = "/home/test/foo/"
	allowed, err = st.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)

	// check that there are 2 rules for read but the write rule was coalesced
	paths := st.MapsForUidAndLabelAndPermission(1000, "snap.lxd.lxd", "read").AllowWithSubdirs
	c.Assert(paths, HasLen, 2)
	paths = st.MapsForUidAndLabelAndPermission(1000, "snap.lxd.lxd", "write").AllowWithSubdirs
	c.Assert(paths, HasLen, 1)
	paths = st.MapsForUidAndLabelAndPermission(1000, "snap.lxd.lxd", "execute").AllowWithSubdirs
	c.Assert(paths, HasLen, 1)
}

func (s *storageSuite) TestPermissionsComplex(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
	}
	extras := map[storage.ExtrasKey]string{}

	allow := true
	req.Path = "/home/test/foo/script.sh"
	req.Permission = apparmor.MayReadPermission
	extras[storage.ExtrasAllowExtraPerms] = "write,execute"
	_, err := st.Set(req, allow, extras)
	c.Assert(err, Equals, nil)
	/*
		c.Assert(len(deleted), Equals, 3)
		emptyMap := make(map[string]bool)
		emptyDeleted := storage.permissionDB{
			Allow: make(map[string]bool),
			AllowWithDir: make(map[string]bool),
			AllowWithSubdirs: make(map[string]bool),
		}
		c.Assert(reflect.DeepEqual(*deleted["read"], emptyDeleted), Equals, true, Commentf(`deleted["read"] should be empty: %+v`, deleted["read"]))
		c.Assert(reflect.DeepEqual(deleted["read"].Allow, emptyMap), Equals, true, Commentf(`deleted["read"].Allow should be empty: %+v`, deleted["read"].Allow))
		c.Assert(reflect.DeepEqual(deleted["read"].AllowWithDir, emptyMap), Equals, true, Commentf(`deleted["read"].AllowWithDir should be empty: %+v`, deleted["read"].AllowWithDir))
		c.Assert(reflect.DeepEqual(deleted["read"].AllowWithSubdirs, emptyMap), Equals, true, Commentf(`deleted["read"].AllowWithSubdirs should be empty: %+v`, deleted["read"].AllowWithSubdirs))
		c.Assert(reflect.DeepEqual(deleted["write"].Allow, emptyMap), Equals, true, Commentf(`deleted["read"].Allow should be empty: %+v`, deleted["read"].Allow))
		c.Assert(reflect.DeepEqual(deleted["write"].AllowWithDir, emptyMap), Equals, true, Commentf(`deleted["read"].AllowWithDir should be empty: %+v`, deleted["read"].AllowWithDir))
		c.Assert(reflect.DeepEqual(deleted["write"].AllowWithSubdirs, emptyMap), Equals, true, Commentf(`deleted["read"].AllowWithSubdirs should be empty: %+v`, deleted["read"].AllowWithSubdirs))
		c.Assert(reflect.DeepEqual(deleted["execute"].Allow, emptyMap), Equals, true, Commentf(`deleted["read"].Allow should be empty: %+v`, deleted["read"].Allow))
		c.Assert(reflect.DeepEqual(deleted["execute"].AllowWithDir, emptyMap), Equals, true, Commentf(`deleted["read"].AllowWithDir should be empty: %+v`, deleted["read"].AllowWithDir))
		c.Assert(reflect.DeepEqual(deleted["execute"].AllowWithSubdirs, emptyMap), Equals, true, Commentf(`deleted["read"].AllowWithSubdirs should be empty: %+v`, deleted["read"].AllowWithSubdirs))
	*/

	allowed, err := st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission | apparmor.MayAppendPermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	allow = false
	req.Path = "/home/test/foo/"
	req.Permission = apparmor.MayWritePermission | apparmor.MayExecutePermission
	extras[storage.ExtrasDenyExtraPerms] = "create"
	extras[storage.ExtrasDenyWithDir] = "yes"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, Equals, nil)

	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	req.Path = "/home/test/foo/script.sh"
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	req.Permission = apparmor.MayReadPermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	req.Permission = apparmor.MayReadPermission | apparmor.MayOpenPermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)

	allow = true
	req.Path = "/home/test/"
	req.Permission = apparmor.MayWritePermission
	extras[storage.ExtrasAllowExtraPerms] = "read"
	extras[storage.ExtrasAllowWithSubdirs] = "yes"
	_, err = st.Set(req, allow, extras)
	c.Assert(err, Equals, nil)

	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Path = "/home/test/foo"
	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	req.Path = "/home/test/foo/script.sh"
	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission | apparmor.MayCreatePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, false)

	req.Path = "/home/test/foo/bar/baz.txt"
	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, nil)
	c.Assert(allowed, Equals, true)

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission | apparmor.MayExecutePermission
	allowed, err = st.Get(req)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)
	c.Assert(allowed, Equals, false)
}

func (s *storageSuite) TestWhichPermissions(c *C) {
	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo",
		Permission: apparmor.MayReadPermission,
	}

	allow := true
	extras := make(map[storage.ExtrasKey]string)

	c.Assert(reflect.DeepEqual(storage.WhichPermissions(req, allow, extras), []string{"read"}), Equals, true, Commentf("WhichPermissions output: %q", storage.WhichPermissions(req, allow, extras)))

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	c.Assert(reflect.DeepEqual(storage.WhichPermissions(req, allow, extras), []string{"write", "read"}), Equals, true, Commentf("WhichPermissions output: %q", storage.WhichPermissions(req, allow, extras)))

	req.Permission = apparmor.MayExecutePermission
	extras[storage.ExtrasAllowExtraPerms] = "read,write"
	c.Assert(reflect.DeepEqual(storage.WhichPermissions(req, allow, extras), []string{"execute", "read", "write"}), Equals, true, Commentf("WhichPermissions output: %q", storage.WhichPermissions(req, allow, extras)))

	req.Permission = apparmor.MayReadPermission | apparmor.MayWritePermission
	extras[storage.ExtrasAllowExtraPerms] = "read,write,execute"
	c.Assert(reflect.DeepEqual(storage.WhichPermissions(req, allow, extras), []string{"write", "read", "execute"}), Equals, true, Commentf("WhichPermissions output: %q", storage.WhichPermissions(req, allow, extras)))
}

func (s *storageSuite) TestGetDoesNotCorruptPath(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo/",
		Permission: apparmor.MayReadPermission,
	}

	c.Assert(req.Path, Equals, "/home/test/foo/")

	allow, err := st.Get(req)

	c.Assert(allow, Equals, false)
	c.Assert(err, Equals, storage.ErrNoSavedDecision)

	c.Assert(req.Path, Equals, "/home/test/foo/")
}

func (s *storageSuite) TestLoadSave(c *C) {
	st := storage.New()

	req := &notifier.Request{
		Label:      "snap.lxd.lxd",
		SubjectUid: 1000,
		Path:       "/home/test/foo",
		Permission: apparmor.MayReadPermission,
	}
	allow := true
	_, err := st.Set(req, allow, nil)
	c.Assert(err, IsNil)

	// st2 will read DB from the previous storage
	st2 := storage.New()
	allowed, err := st2.Get(req)
	c.Assert(err, IsNil)
	c.Assert(allowed, Equals, true)
}
