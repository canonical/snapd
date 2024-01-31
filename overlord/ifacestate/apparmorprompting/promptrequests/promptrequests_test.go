package promptrequests_test

import (
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/promptrequests"
	"github.com/snapcore/snapd/overlord/state"
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
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Fatalf("unexpected notice with userID %d and ID %s", userID, requestID)
		return nil
	}
	prdb := promptrequests.New(notifyRequest)
	c.Assert(prdb.PerUser, HasLen, 0)
}

func (s *promptrequestsSuite) TestAddOrMergeRequests(c *C) {
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 1)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)
	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq1 := &listener.Request{}
	listenerReq2 := &listener.Request{}
	listenerReq3 := &listener.Request{}

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 0)

	before := time.Now()
	req1, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	after := time.Now()
	c.Assert(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req1.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	req2, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Assert(merged, Equals, true)
	c.Assert(req2, Equals, req1)

	// Merged requests should not trigger notice
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	timestamp, err := common.TimestampToTime(req1.Timestamp)
	c.Assert(err, IsNil)
	c.Check(timestamp.After(before), Equals, true)
	c.Check(timestamp.Before(after), Equals, true)

	c.Check(req1.Snap, Equals, snap)
	c.Check(req1.App, Equals, app)
	c.Check(req1.Interface, Equals, iface)
	c.Check(req1.Path, Equals, path)
	c.Check(req1.Permissions, DeepEquals, permissions)

	stored = rdb.Requests(user)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, req1)

	storedReq, err := rdb.RequestWithID(user, req1.ID)
	c.Check(err, IsNil)
	c.Check(storedReq, Equals, req1)

	// Looking up request should not trigger notice
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	req3, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, true)
	c.Check(req3, Equals, req1)

	// Merged requests should not trigger notice
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
}

func (s *promptrequestsSuite) TestRequestWithIDErrors(c *C) {
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 1)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)
	snap := "nextcloud"
	app := "occ"
	iface := "system-files"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	req, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	result, err := rdb.RequestWithID(user, req.ID)
	c.Check(err, IsNil)
	c.Check(result, Equals, req)

	result, err = rdb.RequestWithID(user, "foo")
	c.Check(err, Equals, promptrequests.ErrRequestIDNotFound)
	c.Check(result, IsNil)

	result, err = rdb.RequestWithID(user+1, "foo")
	c.Check(err, Equals, promptrequests.ErrUserNotFound)
	c.Check(result, IsNil)

	// Looking up requests (with or without errors) should not trigger notices
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
}

func (s *promptrequestsSuite) TestReply(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan interface{}, 2)
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 4)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)
	snap := "nextcloud"
	app := "occ"
	iface := "personal-files"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq1 := &listener.Request{}
	listenerReq2 := &listener.Request{}

	req1, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req1.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	req2, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, true)
	c.Check(req2, Equals, req1)

	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	outcome := common.OutcomeAllow
	repliedReq, err := rdb.Reply(user, req1.ID, outcome)
	c.Check(err, IsNil)
	for _, listenerReq := range []*listener.Request{listenerReq1, listenerReq2} {
		c.Check(repliedReq, Equals, req1)
		receivedReq := <-listenerReqChan
		c.Check(receivedReq, Equals, listenerReq)
		result := <-replyChan
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, true)
	}

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, repliedReq.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	listenerReq1 = &listener.Request{}
	listenerReq2 = &listener.Request{}

	req1, merged = rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req1.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	req2, merged = rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, true)
	c.Check(req2, Equals, req1)

	// Merged requests should not trigger notice
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	outcome = common.OutcomeDeny
	repliedReq, err = rdb.Reply(user, req1.ID, outcome)
	for _, listenerReq := range []*listener.Request{listenerReq1, listenerReq2} {
		c.Check(err, IsNil)
		c.Check(repliedReq, Equals, req1)
		receivedReq := <-listenerReqChan
		c.Check(receivedReq, Equals, listenerReq)
		result := <-replyChan
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, false)
	}

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, repliedReq.ID)
	requestNoticeIDs = requestNoticeIDs[1:]
}

func (s *promptrequestsSuite) TestReplyErrors(c *C) {
	fakeError := fmt.Errorf("fake reply error")
	restore := promptrequests.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		return fakeError
	})
	defer restore()

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 1)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)
	snap := "nextcloud"
	app := "occ"
	iface := "removable-media"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	req, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	outcome := common.OutcomeAllow

	_, err := rdb.Reply(user, "foo", outcome)
	c.Check(err, Equals, promptrequests.ErrRequestIDNotFound)

	_, err = rdb.Reply(user+1, "foo", outcome)
	c.Check(err, Equals, promptrequests.ErrUserNotFound)

	_, err = rdb.Reply(user, req.ID, common.OutcomeUnset)
	c.Check(err, Equals, common.ErrInvalidOutcome)

	_, err = rdb.Reply(user, req.ID, outcome)
	c.Check(err, Equals, fakeError)

	// Failed replies should not trigger notice
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
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

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 6)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)

	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	listenerReq1 := &listener.Request{}
	req1, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	listenerReq2 := &listener.Request{}
	req2, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionRead}
	listenerReq3 := &listener.Request{}
	req3, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionOpen}
	listenerReq4 := &listener.Request{}
	req4, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 4, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req1.ID)
	c.Check(requestNoticeIDs[1], Equals, req2.ID)
	c.Check(requestNoticeIDs[2], Equals, req3.ID)
	c.Check(requestNoticeIDs[3], Equals, req4.ID)
	requestNoticeIDs = requestNoticeIDs[4:]

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := rdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, req2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, req3.ID), Equals, true)

	c.Assert(requestNoticeIDs, HasLen, 2, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(strutil.ListContains(requestNoticeIDs, req2.ID), Equals, true)
	c.Check(strutil.ListContains(requestNoticeIDs, req3.ID), Equals, true)
	requestNoticeIDs = requestNoticeIDs[2:]

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

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 6)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)

	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	listenerReq1 := &listener.Request{}
	req1, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	listenerReq2 := &listener.Request{}
	req2, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionRead}
	listenerReq3 := &listener.Request{}
	req3, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionOpen}
	listenerReq4 := &listener.Request{}
	req4, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 4, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req1.ID)
	c.Check(requestNoticeIDs[1], Equals, req2.ID)
	c.Check(requestNoticeIDs[2], Equals, req3.ID)
	c.Check(requestNoticeIDs[3], Equals, req4.ID)
	requestNoticeIDs = requestNoticeIDs[4:]

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeDeny
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := rdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, req2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, req3.ID), Equals, true)

	c.Assert(requestNoticeIDs, HasLen, 2, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(strutil.ListContains(requestNoticeIDs, req2.ID), Equals, true)
	c.Check(strutil.ListContains(requestNoticeIDs, req3.ID), Equals, true)
	requestNoticeIDs = requestNoticeIDs[2:]

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
	satisfied, err = rdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)

	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	// The request is not modified (since not fully satisfied), so no notice should be issued
	c.Assert(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

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

	var user uint32 = 1000
	requestNoticeIDs := make([]string, 0, 2)
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		requestNoticeIDs = append(requestNoticeIDs, requestID)
		return nil
	}

	rdb := promptrequests.New(notifyRequest)

	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionRead}
	listenerReq := &listener.Request{}
	req, merged := rdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow

	otherUser := user + 1
	otherSnap := "ldx"
	otherApp := "lxc"
	otherInterface := "system-files"
	otherPattern := "/home/test/Pictures/**.png"
	badPattern := "\\home\\test\\"
	badOutcome := common.OutcomeType("foo")

	stored := rdb.Requests(user)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], Equals, req)

	satisfied, err := rdb.HandleNewRule(otherUser, otherSnap, otherApp, otherInterface, otherPattern, badOutcome, permissions)
	c.Check(err, Equals, common.ErrInvalidOutcome)
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(otherUser, otherSnap, otherApp, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(user, otherSnap, otherApp, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(user, snap, otherApp, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(user, snap, app, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(user, snap, app, iface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(user, snap, app, iface, badPattern, outcome, permissions)
	c.Check(err, ErrorMatches, "syntax error in pattern")
	c.Check(satisfied, HasLen, 0)

	c.Check(requestNoticeIDs, HasLen, 0, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))

	satisfied, err = rdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	c.Assert(requestNoticeIDs, HasLen, 1, Commentf("requestNoticeIDs: %v; rdb.PerUser[%d]: %+v", requestNoticeIDs, user, rdb.PerUser[user]))
	c.Check(requestNoticeIDs[0], Equals, req.ID)
	requestNoticeIDs = requestNoticeIDs[1:]

	satisfiedReq := <-listenerReqChan
	c.Check(satisfiedReq, Equals, listenerReq)
	result := <-replyChan
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)

	stored = rdb.Requests(user)
	c.Check(stored, HasLen, 0)
}
