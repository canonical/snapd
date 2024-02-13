package requestprompts_test

import (
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/requestprompts"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { TestingT(t) }

type requestpromptsSuite struct {
	tmpdir string
}

var _ = Suite(&requestpromptsSuite{})

func (s *requestpromptsSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *requestpromptsSuite) TestNew(c *C) {
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Fatalf("unexpected notice with userID %d and ID %s", userID, promptID)
		return nil
	}
	pdb := requestprompts.New(notifyPrompt)
	c.Assert(pdb.PerUser, HasLen, 0)
}

func (s *requestpromptsSuite) TestAddOrMergePrompt(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 1)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)
	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq1 := &listener.Request{}
	listenerReq2 := &listener.Request{}
	listenerReq3 := &listener.Request{}

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 0)

	before := time.Now()
	prompt1, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	after := time.Now()
	c.Assert(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt1.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	prompt2, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Assert(merged, Equals, true)
	c.Assert(prompt2, Equals, prompt1)

	// Merged prompts should not trigger notice
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	timestamp, err := common.TimestampToTime(prompt1.Timestamp)
	c.Assert(err, IsNil)
	c.Check(timestamp.After(before), Equals, true)
	c.Check(timestamp.Before(after), Equals, true)

	c.Check(prompt1.Snap, Equals, snap)
	c.Check(prompt1.App, Equals, app)
	c.Check(prompt1.Interface, Equals, iface)
	c.Check(prompt1.Path, Equals, path)
	c.Check(prompt1.Permissions, DeepEquals, permissions)

	stored = pdb.Prompts(user)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)

	storedPrompt, err := pdb.PromptWithID(user, prompt1.ID)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt1)

	// Looking up prompt should not trigger notice
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	prompt3, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)

	// Merged prompts should not trigger notice
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
}

func (s *requestpromptsSuite) TestPromptWithIDErrors(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 1)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)
	snap := "nextcloud"
	app := "occ"
	iface := "system-files"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	prompt, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	result, err := pdb.PromptWithID(user, prompt.ID)
	c.Check(err, IsNil)
	c.Check(result, Equals, prompt)

	result, err = pdb.PromptWithID(user, "foo")
	c.Check(err, Equals, requestprompts.ErrPromptIDNotFound)
	c.Check(result, IsNil)

	result, err = pdb.PromptWithID(user+1, "foo")
	c.Check(err, Equals, requestprompts.ErrUserNotFound)
	c.Check(result, IsNil)

	// Looking up prompts (with or without errors) should not trigger notices
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
}

func (s *requestpromptsSuite) TestReply(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan interface{}, 2)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 4)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)
	snap := "nextcloud"
	app := "occ"
	iface := "personal-files"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq1 := &listener.Request{}
	listenerReq2 := &listener.Request{}

	prompt1, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt1.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	prompt2, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, true)
	c.Check(prompt2, Equals, prompt1)

	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	outcome := common.OutcomeAllow
	repliedPrompt, err := pdb.Reply(user, prompt1.ID, outcome)
	c.Check(err, IsNil)
	c.Check(repliedPrompt, Equals, prompt1)
	for _, listenerReq := range []*listener.Request{listenerReq1, listenerReq2} {
		receivedReq := <-listenerReqChan
		c.Check(receivedReq, Equals, listenerReq)
		result := <-replyChan
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, true)
	}

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, repliedPrompt.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	listenerReq1 = &listener.Request{}
	listenerReq2 = &listener.Request{}

	prompt1, merged = pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt1.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	prompt2, merged = pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, true)
	c.Check(prompt2, Equals, prompt1)

	// Merged prompts should not trigger notice
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	outcome = common.OutcomeDeny
	repliedPrompt, err = pdb.Reply(user, prompt1.ID, outcome)
	c.Check(err, IsNil)
	c.Check(repliedPrompt, Equals, prompt1)
	for _, listenerReq := range []*listener.Request{listenerReq1, listenerReq2} {
		receivedReq := <-listenerReqChan
		c.Check(receivedReq, Equals, listenerReq)
		result := <-replyChan
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, false)
	}

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, repliedPrompt.ID)
	promptNoticeIDs = promptNoticeIDs[1:]
}

func (s *requestpromptsSuite) TestReplyErrors(c *C) {
	fakeError := fmt.Errorf("fake reply error")
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		return fakeError
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 1)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)
	snap := "nextcloud"
	app := "occ"
	iface := "removable-media"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}

	listenerReq := &listener.Request{}

	prompt, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	outcome := common.OutcomeAllow

	_, err := pdb.Reply(user, "foo", outcome)
	c.Check(err, Equals, requestprompts.ErrPromptIDNotFound)

	_, err = pdb.Reply(user+1, "foo", outcome)
	c.Check(err, Equals, requestprompts.ErrUserNotFound)

	_, err = pdb.Reply(user, prompt.ID, common.OutcomeUnset)
	c.Check(err, Equals, common.ErrInvalidOutcome)

	_, err = pdb.Reply(user, prompt.ID, outcome)
	c.Check(err, Equals, fakeError)

	// Failed replies should not trigger notice
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
}

func (s *requestpromptsSuite) TestHandleNewRuleAllowPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan interface{}, 2)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 6)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)

	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	listenerReq1 := &listener.Request{}
	prompt1, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	listenerReq2 := &listener.Request{}
	prompt2, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionRead}
	listenerReq3 := &listener.Request{}
	prompt3, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionOpen}
	listenerReq4 := &listener.Request{}
	prompt4, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 4, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt1.ID)
	c.Check(promptNoticeIDs[1], Equals, prompt2.ID)
	c.Check(promptNoticeIDs[2], Equals, prompt3.ID)
	c.Check(promptNoticeIDs[3], Equals, prompt4.ID)
	promptNoticeIDs = promptNoticeIDs[4:]

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := pdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt3.ID), Equals, true)

	c.Assert(promptNoticeIDs, HasLen, 2, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(strutil.ListContains(promptNoticeIDs, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(promptNoticeIDs, prompt3.ID), Equals, true)
	promptNoticeIDs = promptNoticeIDs[2:]

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

	stored = pdb.Prompts(user)
	c.Assert(stored, HasLen, 2)
}

func (s *requestpromptsSuite) TestHandleNewRuleDenyPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan interface{}, 2)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 6)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)

	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"

	permissions := []common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead}
	listenerReq1 := &listener.Request{}
	prompt1, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	listenerReq2 := &listener.Request{}
	prompt2, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionRead}
	listenerReq3 := &listener.Request{}
	prompt3, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []common.PermissionType{common.PermissionOpen}
	listenerReq4 := &listener.Request{}
	prompt4, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 4, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt1.ID)
	c.Check(promptNoticeIDs[1], Equals, prompt2.ID)
	c.Check(promptNoticeIDs[2], Equals, prompt3.ID)
	c.Check(promptNoticeIDs[3], Equals, prompt4.ID)
	promptNoticeIDs = promptNoticeIDs[4:]

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 4)

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeDeny
	permissions = []common.PermissionType{common.PermissionWrite, common.PermissionRead, common.PermissionAppend}

	satisfied, err := pdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt3.ID), Equals, true)

	c.Assert(promptNoticeIDs, HasLen, 2, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(strutil.ListContains(promptNoticeIDs, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(promptNoticeIDs, prompt3.ID), Equals, true)
	promptNoticeIDs = promptNoticeIDs[2:]

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

	stored = pdb.Prompts(user)
	c.Check(stored, HasLen, 2)

	// check that denying the final missing permission does not deny the whole rule.
	// TODO: change this behaviour?
	permissions = []common.PermissionType{common.PermissionExecute}
	satisfied, err = pdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)

	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	// The prompt is not modified (since not fully satisfied), so no notice should be issued
	c.Assert(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
}

func (s *requestpromptsSuite) TestHandleNewRuleNonMatches(c *C) {
	listenerReqChan := make(chan *listener.Request, 1)
	replyChan := make(chan interface{}, 1)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	var user uint32 = 1000
	promptNoticeIDs := make([]string, 0, 2)
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		c.Check(userID, Equals, user)
		promptNoticeIDs = append(promptNoticeIDs, promptID)
		return nil
	}

	pdb := requestprompts.New(notifyPrompt)

	snap := "nextcloud"
	app := "occ"
	iface := "home"
	path := "/home/test/Documents/foo.txt"
	permissions := []common.PermissionType{common.PermissionRead}
	listenerReq := &listener.Request{}
	prompt, merged := pdb.AddOrMerge(user, snap, app, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	pathPattern := "/home/test/Documents/**"
	outcome := common.OutcomeAllow

	otherUser := user + 1
	otherSnap := "ldx"
	otherApp := "lxc"
	otherInterface := "system-files"
	otherPattern := "/home/test/Pictures/**.png"
	badPattern := "\\home\\test\\"
	badOutcome := common.OutcomeType("foo")

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], Equals, prompt)

	satisfied, err := pdb.HandleNewRule(otherUser, otherSnap, otherApp, otherInterface, otherPattern, badOutcome, permissions)
	c.Check(err, Equals, common.ErrInvalidOutcome)
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(otherUser, otherSnap, otherApp, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(user, otherSnap, otherApp, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(user, snap, otherApp, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(user, snap, app, otherInterface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(user, snap, app, iface, otherPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(user, snap, app, iface, badPattern, outcome, permissions)
	c.Check(err, ErrorMatches, "syntax error in pattern")
	c.Check(satisfied, HasLen, 0)

	c.Check(promptNoticeIDs, HasLen, 0, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))

	satisfied, err = pdb.HandleNewRule(user, snap, app, iface, pathPattern, outcome, permissions)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	c.Assert(promptNoticeIDs, HasLen, 1, Commentf("promptNoticeIDs: %v; pdb.PerUser[%d]: %+v", promptNoticeIDs, user, pdb.PerUser[user]))
	c.Check(promptNoticeIDs[0], Equals, prompt.ID)
	promptNoticeIDs = promptNoticeIDs[1:]

	satisfiedReq := <-listenerReqChan
	c.Check(satisfiedReq, Equals, listenerReq)
	result := <-replyChan
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)

	stored = pdb.Prompts(user)
	c.Check(stored, HasLen, 0)
}
