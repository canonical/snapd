package promptrequests_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/promptrequests"
	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { TestingT(t) }

type promptrequestsSuite struct {
	tmpdir string
}

var _ = Suite(&promptrequestsSuite{})

func (s *promptrequestsSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *promptrequestsSuite) TestNew(c *C) {
	prdb := promptrequests.New()
	c.Assert(prdb.PerUser, HasLen, 0)
}

func (s *promptrequestsSuite) TestAddRequests(c *C) {
	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	replyChan := make(chan bool)

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 0)

	before := time.Now()
	req := rdb.Add(user, snap, app, path, permissions, replyChan)
	after := time.Now()

	timestamp, err := common.TimestampToTime(req.Timestamp)
	c.Assert(err, IsNil)
	c.Assert(timestamp.After(before), Equals, true)
	c.Assert(timestamp.Before(after), Equals, true)

	c.Assert(req.Snap, Equals, snap)
	c.Assert(req.App, Equals, app)
	c.Assert(req.Path, Equals, path)
	c.Assert(req.Permissions, DeepEquals, permissions)

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], DeepEquals, req)

	storedReq, err := rdb.RequestWithId(user, req.Id)
	c.Assert(err, IsNil)
	c.Assert(storedReq, DeepEquals, req)
}

func (s *promptrequestsSuite) TestRequestWithIdErrors(c *C) {
	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	replyChan := make(chan bool)

	req := rdb.Add(user, snap, app, path, permissions, replyChan)

	result, err := rdb.RequestWithId(user, req.Id)
	c.Assert(err, IsNil)
	c.Assert(result, DeepEquals, req)

	result, err = rdb.RequestWithId(user, "foo")
	c.Assert(err, Equals, promptrequests.ErrRequestIdNotFound)
	c.Assert(result, IsNil)

	result, err = rdb.RequestWithId(user+1, "foo")
	c.Assert(err, Equals, promptrequests.ErrUserNotFound)
	c.Assert(result, IsNil)
}

func (s *promptrequestsSuite) TestReply(c *C) {
	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	replyChan := make(chan bool)

	req := rdb.Add(user, snap, app, path, permissions, replyChan)

	outcome := common.OutcomeAllow
	go rdb.Reply(user, req.Id, outcome)
	result := <-replyChan
	c.Assert(result, Equals, true)

	req = rdb.Add(user, snap, app, path, permissions, replyChan)

	outcome = common.OutcomeDeny
	go rdb.Reply(user, req.Id, outcome)
	result = <-replyChan
	c.Assert(result, Equals, false)
}

func (s *promptrequestsSuite) TestReplyErrors(c *C) {
	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	replyChan := make(chan bool)

	_ = rdb.Add(user, snap, app, path, permissions, replyChan)

	outcome := common.OutcomeAllow

	err := rdb.Reply(user, "foo", outcome)
	c.Assert(err, Equals, promptrequests.ErrRequestIdNotFound)

	err = rdb.Reply(user+1, "foo", outcome)
	c.Assert(err, Equals, promptrequests.ErrUserNotFound)
}

func (s *promptrequestsSuite) TestHandleNewRuleAllowPermissions(c *C) {
	rdb := promptrequests.New()

	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	replyChan1 := make(chan bool)
	_ = rdb.Add(user, snap, app, path, permissions, replyChan1)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	replyChan2 := make(chan bool, 1)
	req2 := rdb.Add(user, snap, app, path, permissions, replyChan2)

	permissions = []common.PermissionType{common.PermissionRead}
	replyChan3 := make(chan bool, 1)
	req3 := rdb.Add(user, snap, app, path, permissions, replyChan3)

	permissions = []common.PermissionType{common.PermissionOpen}
	replyChan4 := make(chan bool)
	_ = rdb.Add(user, snap, app, path, permissions, replyChan4)

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 2)
	c.Assert(strutil.ListContains(satisfied, req2.Id), Equals, true)
	c.Assert(strutil.ListContains(satisfied, req3.Id), Equals, true)

	result2 := <-replyChan2
	result3 := <-replyChan3
	c.Assert(result2, Equals, true)
	c.Assert(result3, Equals, true)

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 2)
}

func (s *promptrequestsSuite) TestHandleNewRuleDenyPermissions(c *C) {
	rdb := promptrequests.New()

	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	replyChan1 := make(chan bool)
	_ = rdb.Add(user, snap, app, path, permissions, replyChan1)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	replyChan2 := make(chan bool, 1)
	req2 := rdb.Add(user, snap, app, path, permissions, replyChan2)

	permissions = []common.PermissionType{common.PermissionRead}
	replyChan3 := make(chan bool, 1)
	req3 := rdb.Add(user, snap, app, path, permissions, replyChan3)

	permissions = []common.PermissionType{common.PermissionOpen}
	replyChan4 := make(chan bool)
	_ = rdb.Add(user, snap, app, path, permissions, replyChan4)

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeDeny
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 2)
	c.Assert(strutil.ListContains(satisfied, req2.Id), Equals, true)
	c.Assert(strutil.ListContains(satisfied, req3.Id), Equals, true)

	result2 := <-replyChan2
	result3 := <-replyChan3
	c.Assert(result2, Equals, false)
	c.Assert(result3, Equals, false)

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 2)

	// check that denying the final missing permission does not deny the whole rule.
	// TODO: change this behaviour?
	permissions = []common.PermissionType{common.PermissionExecute}
	satisfied, err = rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)

	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 0)
}

func (s *promptrequestsSuite) TestHandleNewRuleNonMatches(c *C) {
	rdb := promptrequests.New()

	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionRead}
	replyChan := make(chan bool, 1)
	req := rdb.Add(user, snap, app, path, permissions, replyChan)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow

	otherUser := user + 1
	otherSnap := "ldx"
	otherApp := "lxc"
	otherPattern := "/home/test/Pictures/**.png"
	badPattern := "\\home\\test\\"
	badOutcome := common.OutcomeType("foo")

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], DeepEquals, req)

	satisfied, err := rdb.HandleNewRule(otherUser, otherSnap, otherApp, otherPattern, badOutcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, otherSnap, otherApp, otherPattern, badOutcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, otherApp, otherPattern, badOutcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, otherPattern, badOutcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, badPattern, badOutcome, permissions)
	c.Assert(err, ErrorMatches, "syntax error in pattern")
	c.Assert(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, pathPattern, badOutcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	result := <-replyChan
	c.Assert(result, Equals, true)

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 0)
}
