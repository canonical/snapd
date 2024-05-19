package requestprompts_test

import (
	"fmt"
	"sort"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { TestingT(t) }

type requestpromptsSuite struct {
	defaultNotifyPrompt func(userID uint32, promptID string) error
	defaultUser         uint32
	noticePromptIDs     []string

	tmpdir string
}

var _ = Suite(&requestpromptsSuite{})

func (s *requestpromptsSuite) SetUpTest(c *C) {
	s.defaultUser = 1000
	s.defaultNotifyPrompt = func(userID uint32, promptID string) error {
		c.Check(userID, Equals, s.defaultUser)
		s.noticePromptIDs = append(s.noticePromptIDs, promptID)
		return nil
	}
	s.noticePromptIDs = make([]string, 0)
}

func (s *requestpromptsSuite) TestNew(c *C) {
	notifyPrompt := func(userID uint32, promptID string) error {
		c.Fatalf("unexpected notice with userID %d and ID %s", userID, promptID)
		return nil
	}
	pdb := requestprompts.New(notifyPrompt)
	c.Check(pdb.MaxID(), Equals, uint64(0))
}

func (s *requestpromptsSuite) TestAddOrMerge(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "home"
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq1 := &listener.Request{}
	listenerReq2 := &listener.Request{}
	listenerReq3 := &listener.Request{}

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 0)

	before := time.Now()
	prompt1, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq1)
	after := time.Now()
	c.Assert(merged, Equals, false)

	s.checkNewNotices(c, []string{prompt1.ID})
	c.Check(pdb.MaxID(), Equals, uint64(1))

	prompt2, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq2)
	c.Assert(merged, Equals, true)
	c.Assert(prompt2, Equals, prompt1)

	// Merged prompts should not trigger notice
	s.checkNewNotices(c, []string{})
	// Merged prompts should not advance the max ID
	c.Check(pdb.MaxID(), Equals, uint64(1))

	c.Check(prompt1.Timestamp.After(before), Equals, true)
	c.Check(prompt1.Timestamp.Before(after), Equals, true)

	c.Check(prompt1.Snap, Equals, snap)
	c.Check(prompt1.Interface, Equals, iface)
	c.Check(prompt1.Constraints.Path, Equals, path)
	c.Check(prompt1.Constraints.Permissions, DeepEquals, permissions)

	stored = pdb.Prompts(user)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)

	storedPrompt, err := pdb.PromptWithID(user, prompt1.ID)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt1)

	// Looking up prompt should not trigger notice
	s.checkNewNotices(c, []string{})

	prompt3, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)

	// Merged prompts should not trigger notice
	s.checkNewNotices(c, []string{})
	// Merged prompts should not advance the max ID
	c.Check(pdb.MaxID(), Equals, uint64(1))
}

func (s *requestpromptsSuite) checkNewNotices(c *C, expectedPromptIDs []string) {
	c.Check(s.noticePromptIDs, DeepEquals, expectedPromptIDs)
	s.noticePromptIDs = s.noticePromptIDs[:0]
}

func (s *requestpromptsSuite) checkNewNoticesUnordered(c *C, expectedPromptIDs []string) {
	sort.Strings(s.noticePromptIDs)
	sort.Strings(expectedPromptIDs)
	s.checkNewNotices(c, expectedPromptIDs)
}

func (s *requestpromptsSuite) TestPromptWithIDErrors(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "system-files"
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}

	prompt, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	s.checkNewNotices(c, []string{prompt.ID})

	result, err := pdb.PromptWithID(user, prompt.ID)
	c.Check(err, IsNil)
	c.Check(result, Equals, prompt)

	result, err = pdb.PromptWithID(user, "foo")
	c.Check(err, ErrorMatches, "cannot find prompt for UID 1000 with the given ID:.*")
	c.Check(result, IsNil)

	result, err = pdb.PromptWithID(user+1, "foo")
	c.Check(err, ErrorMatches, "cannot find prompts for the given UID:.*")
	c.Check(result, IsNil)

	// Looking up prompts (with or without errors) should not trigger notices
	s.checkNewNotices(c, []string{})
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

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "personal-files"
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	for _, outcome := range []prompting.OutcomeType{prompting.OutcomeAllow, prompting.OutcomeDeny} {
		listenerReq1 := &listener.Request{}
		listenerReq2 := &listener.Request{}

		prompt1, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq1)
		c.Check(merged, Equals, false)

		s.checkNewNotices(c, []string{prompt1.ID})

		prompt2, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq2)
		c.Check(merged, Equals, true)
		c.Check(prompt2, Equals, prompt1)

		// Merged prompts should not trigger notice
		s.checkNewNotices(c, []string{})

		repliedPrompt, err := pdb.Reply(user, prompt1.ID, outcome)
		c.Check(err, IsNil)
		c.Check(repliedPrompt, Equals, prompt1)
		for _, listenerReq := range []*listener.Request{listenerReq1, listenerReq2} {
			receivedReq, result, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
			c.Check(err, IsNil)
			c.Check(receivedReq, Equals, listenerReq)
			allowed, ok := result.(bool)
			c.Check(ok, Equals, true)
			expected, err := outcome.IsAllow()
			c.Check(err, IsNil)
			c.Check(allowed, Equals, expected)
		}

		s.checkNewNotices(c, []string{repliedPrompt.ID})
	}
}

func (s *requestpromptsSuite) waitForListenerReqAndReply(c *C, listenerReqChan <-chan *listener.Request, replyChan <-chan interface{}) (req *listener.Request, reply interface{}, err error) {
	select {
	case req = <-listenerReqChan:
	case <-time.NewTimer(10 * time.Second).C:
		err = fmt.Errorf("failed to receive request over channel")
	}
	select {
	case reply = <-replyChan:
	case <-time.NewTimer(10 * time.Second).C:
		err = fmt.Errorf("failed to receive reply over channel")
	}
	return req, reply, err
}

func (s *requestpromptsSuite) TestReplyErrors(c *C) {
	fakeError := fmt.Errorf("fake reply error")
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		return fakeError
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "removable-media"
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}

	prompt, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	s.checkNewNotices(c, []string{prompt.ID})

	outcome := prompting.OutcomeAllow

	_, err := pdb.Reply(user, "foo", outcome)
	c.Check(err, ErrorMatches, "cannot find prompt for UID 1000 with the given ID:.*")

	_, err = pdb.Reply(user+1, "foo", outcome)
	c.Check(err, ErrorMatches, "cannot find prompts for the given UID:.*")

	_, err = pdb.Reply(user, prompt.ID, prompting.OutcomeUnset)
	c.Check(err, ErrorMatches, `internal error: invalid outcome.*`)

	_, err = pdb.Reply(user, prompt.ID, outcome)
	c.Check(err, Equals, fakeError)

	// Failed replies should not trigger notice
	s.checkNewNotices(c, []string{})
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

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "home"
	path := "/home/test/Documents/foo.txt"

	permissions := []string{"read", "write", "execute"}
	listenerReq1 := &listener.Request{}
	prompt1, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []string{"read", "write"}
	listenerReq2 := &listener.Request{}
	prompt2, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []string{"read"}
	listenerReq3 := &listener.Request{}
	prompt3, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []string{"open"}
	listenerReq4 := &listener.Request{}
	prompt4, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	s.checkNewNotices(c, []string{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID})

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 4)

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	permissions = []string{"read", "write", "append"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	outcome := prompting.OutcomeAllow

	satisfied, err := pdb.HandleNewRule(user, snap, iface, constraints, outcome)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt3.ID), Equals, true)

	// Read and write permissions of prompt1 satisfied, so notice re-issued,
	// but it has one remaining permission. prompt2 and prompt3 fully satisfied.
	s.checkNewNoticesUnordered(c, []string{prompt1.ID, prompt2.ID, prompt3.ID})

	for i := 0; i < 2; i++ {
		satisfiedReq, result, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
		c.Check(err, IsNil)
		if satisfiedReq != listenerReq2 && satisfiedReq != listenerReq3 {
			c.Errorf("unexpected request satisfied by new rule")
		}
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, true)
	}

	stored = pdb.Prompts(user)
	c.Assert(stored, HasLen, 2)

	// Check that allowing the final missing permission allows the prompt.
	permissions = []string{"execute"}
	constraints = &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	satisfied, err = pdb.HandleNewRule(user, snap, iface, constraints, outcome)

	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 1)
	c.Check(satisfied[0], Equals, prompt1.ID)

	s.checkNewNotices(c, []string{prompt1.ID})

	satisfiedReq, result, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
	c.Check(err, IsNil)
	c.Check(satisfiedReq, Equals, listenerReq1)
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)
}

func (s *requestpromptsSuite) TestHandleNewRuleDenyPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 3)
	replyChan := make(chan interface{}, 3)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		listenerReqChan <- listenerReq
		replyChan <- reply
		return nil
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "home"
	path := "/home/test/Documents/foo.txt"

	permissions := []string{"read", "write", "execute"}
	listenerReq1 := &listener.Request{}
	prompt1, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []string{"read", "write"}
	listenerReq2 := &listener.Request{}
	prompt2, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []string{"read"}
	listenerReq3 := &listener.Request{}
	prompt3, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []string{"open"}
	listenerReq4 := &listener.Request{}
	prompt4, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	s.checkNewNotices(c, []string{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID})

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 4)

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	permissions = []string{"read", "write", "append"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	outcome := prompting.OutcomeDeny

	// If one or more permissions denied each for prompts 1-3, so each is denied
	satisfied, err := pdb.HandleNewRule(user, snap, iface, constraints, outcome)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 3)
	c.Check(strutil.ListContains(satisfied, prompt1.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt3.ID), Equals, true)

	s.checkNewNoticesUnordered(c, []string{prompt1.ID, prompt2.ID, prompt3.ID})

	for i := 0; i < 3; i++ {
		satisfiedReq, result, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
		c.Check(err, IsNil)
		if satisfiedReq != listenerReq1 && satisfiedReq != listenerReq2 && satisfiedReq != listenerReq3 {
			c.Errorf("unexpected request satisfied by new rule")
		}
		allowed, ok := result.(bool)
		c.Check(ok, Equals, true)
		c.Check(allowed, Equals, false)
	}

	stored = pdb.Prompts(user)
	c.Check(stored, HasLen, 1)
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

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	user := s.defaultUser
	snap := "nextcloud"
	iface := "home"
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read"}
	listenerReq := &listener.Request{}
	prompt, merged := pdb.AddOrMerge(user, snap, iface, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	s.checkNewNotices(c, []string{prompt.ID})

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	outcome := prompting.OutcomeAllow

	otherUser := user + 1
	otherSnap := "ldx"
	otherInterface := "system-files"
	otherPattern, err := patterns.ParsePathPattern("/home/test/Pictures/**.png")
	c.Assert(err, IsNil)
	otherConstraints := &prompting.Constraints{
		PathPattern: otherPattern,
		Permissions: permissions,
	}
	badOutcome := prompting.OutcomeType("foo")

	stored := pdb.Prompts(user)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], Equals, prompt)

	satisfied, err := pdb.HandleNewRule(user, snap, iface, constraints, badOutcome)
	c.Check(err, ErrorMatches, `internal error: invalid outcome.*`)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNotices(c, []string{})

	satisfied, err = pdb.HandleNewRule(otherUser, snap, iface, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNotices(c, []string{})

	satisfied, err = pdb.HandleNewRule(user, otherSnap, iface, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNotices(c, []string{})

	satisfied, err = pdb.HandleNewRule(user, snap, otherInterface, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNotices(c, []string{})

	satisfied, err = pdb.HandleNewRule(user, snap, iface, otherConstraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNotices(c, []string{})

	satisfied, err = pdb.HandleNewRule(user, snap, iface, constraints, outcome)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	s.checkNewNotices(c, []string{prompt.ID})

	satisfiedReq, result, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
	c.Check(err, IsNil)
	c.Check(satisfiedReq, Equals, listenerReq)
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)

	stored = pdb.Prompts(user)
	c.Check(stored, HasLen, 0)
}
