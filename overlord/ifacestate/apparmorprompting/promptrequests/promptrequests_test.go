package promptrequests_test

import (
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/promptrequests"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
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
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 0)

	before := time.Now()
	req := rdb.Add(user, snap, app, path, permissions, listenerReq)
	after := time.Now()

	timestamp, err := common.TimestampToTime(req.Timestamp)
	c.Assert(err, IsNil)
	c.Check(timestamp.After(before), Equals, true)
	c.Check(timestamp.Before(after), Equals, true)

	c.Check(req.Snap, Equals, snap)
	c.Check(req.App, Equals, app)
	c.Check(req.Path, Equals, path)
	c.Check(req.Permissions, DeepEquals, permissions)

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, req)

	storedReq, err := rdb.RequestWithID(user, req.ID)
	c.Check(err, IsNil)
	c.Check(storedReq, Equals, req)
}

func (s *promptrequestsSuite) TestRequestWithIDErrors(c *C) {
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	req := rdb.Add(user, snap, app, path, permissions, listenerReq)

	result, err := rdb.RequestWithID(user, req.ID)
	c.Check(err, IsNil)
	c.Check(result, Equals, req)

	result, err = rdb.RequestWithID(user, "foo")
	c.Check(err, Equals, promptrequests.ErrRequestIDNotFound)
	c.Check(result, IsNil)

	result, err = rdb.RequestWithID(user+1, "foo")
	c.Check(err, Equals, promptrequests.ErrUserNotFound)
	c.Check(result, IsNil)
}

func (s *promptrequestsSuite) TestReply(c *C) {
	listenerReqChan := make(chan *listener.Request, 1)
	replyChan := make(chan interface{}, 1)
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	req := rdb.Add(user, snap, app, path, permissions, listenerReq)

	outcome := common.OutcomeAllow
	err := rdb.Reply(user, req.ID, outcome)
	c.Check(err, IsNil)
	receivedReq := <-listenerReqChan
	c.Check(receivedReq, Equals, listenerReq)
	result := <-replyChan
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)

	listenerReq = &listener.Request{}

	req = rdb.Add(user, snap, app, path, permissions, listenerReq)

	outcome = common.OutcomeDeny
	err = rdb.Reply(user, req.ID, outcome)
	c.Check(err, IsNil)
	receivedReq = <-listenerReqChan
	c.Check(receivedReq, Equals, listenerReq)
	result = <-replyChan
	allowed, ok = result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, false)
}

func (s *promptrequestsSuite) TestReplyErrors(c *C) {
	fakeError := fmt.Errorf("fake reply error")
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		return fakeError
	})
	defer restore()

	rdb := promptrequests.New()
	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	req := rdb.Add(user, snap, app, path, permissions, listenerReq)

	outcome := common.OutcomeAllow

	err := rdb.Reply(user, "foo", outcome)
	c.Check(err, Equals, promptrequests.ErrRequestIDNotFound)

	err = rdb.Reply(user+1, "foo", outcome)
	c.Check(err, Equals, promptrequests.ErrUserNotFound)

	err = rdb.Reply(user, req.ID, common.OutcomeUnset)
	c.Check(err, Equals, common.ErrInvalidOutcome)

	err = rdb.Reply(user, req.ID, outcome)
	c.Check(err, Equals, fakeError)
}

func (s *promptrequestsSuite) TestHandleNewRuleAllowPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan interface{}, 2)
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	rdb := promptrequests.New()

	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	listenerReq1 := &listener.Request{}
	_ = rdb.Add(user, snap, app, path, permissions, listenerReq1)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	listenerReq2 := &listener.Request{}
	req2 := rdb.Add(user, snap, app, path, permissions, listenerReq2)

	permissions = []common.PermissionType{common.PermissionRead}
	listenerReq3 := &listener.Request{}
	req3 := rdb.Add(user, snap, app, path, permissions, listenerReq3)

	permissions = []common.PermissionType{common.PermissionOpen}
	listenerReq4 := &listener.Request{}
	_ = rdb.Add(user, snap, app, path, permissions, listenerReq4)

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, req2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, req3.ID), Equals, true)

	for i := 0; i < 2; i++ {
		satisfiedReq := <-listenerReqChan
		switch satisfiedReq {
		case listenerReq2:
		case listenerReq3:
		default:
			c.Errorf("unexpected request satisfied by new rule")
		}
		result := <-replyChan
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, true)
	}

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 2)
}

func (s *promptrequestsSuite) TestHandleNewRuleDenyPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan interface{}, 2)
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	rdb := promptrequests.New()

	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	listenerReq1 := &listener.Request{}
	_ = rdb.Add(user, snap, app, path, permissions, listenerReq1)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	listenerReq2 := &listener.Request{}
	req2 := rdb.Add(user, snap, app, path, permissions, listenerReq2)

	permissions = []common.PermissionType{common.PermissionRead}
	listenerReq3 := &listener.Request{}
	req3 := rdb.Add(user, snap, app, path, permissions, listenerReq3)

	permissions = []common.PermissionType{common.PermissionOpen}
	listenerReq4 := &listener.Request{}
	_ = rdb.Add(user, snap, app, path, permissions, listenerReq4)

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeDeny
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, req2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, req3.ID), Equals, true)

	for i := 0; i < 2; i++ {
		satisfiedReq := <-listenerReqChan
		switch satisfiedReq {
		case listenerReq2:
		case listenerReq3:
		default:
			c.Errorf("unexpected request satisfied by new rule")
		}
		result := <-replyChan
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, false)
	}

	stored = rdb.Requests(user)
	c.Check(stored, HasLen, 2)

	// check that denying the final missing permission does not deny the whole rule.
	// TODO: change this behaviour?
	permissions = []common.PermissionType{common.PermissionExecute}
	satisfied, err = rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)

	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 0)
}

func (s *promptrequestsSuite) TestHandleNewRuleNonMatches(c *C) {
	listenerReqChan := make(chan *listener.Request, 1)
	replyChan := make(chan interface{}, 1)
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	rdb := promptrequests.New()

	var user uint32 = 1000
	snap := "nextcloud"
	app := "occ"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionRead}
	listenerReq := &listener.Request{}
	req := rdb.Add(user, snap, app, path, permissions, listenerReq)

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
	c.Assert(stored[0], Equals, req)

	satisfied, err := rdb.HandleNewRule(otherUser, otherSnap, otherApp, otherPattern, badOutcome, permissions)
	c.Check(err, Equals, common.ErrInvalidOutcome)
	c.Check(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(otherUser, otherSnap, otherApp, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, otherSnap, otherApp, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, otherApp, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, badPattern, outcome, permissions)
	c.Check(err, ErrorMatches, "syntax error in pattern")
	c.Check(satisfied, HasLen, 0)

	satisfied, err = rdb.HandleNewRule(user, snap, app, pathPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	satisfiedReq := <-listenerReqChan
	c.Check(satisfiedReq, Equals, listenerReq)
	result := <-replyChan
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)

	stored = rdb.Requests(user)
	c.Check(stored, HasLen, 0)
}
