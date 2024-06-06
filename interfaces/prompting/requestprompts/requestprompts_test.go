// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package requestprompts_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { TestingT(t) }

type noticeInfo struct {
	promptID string
	data     map[string]string
}

type noticeList []*noticeInfo

// Implements sort.Interface
func (l noticeList) Len() int {
	return len(l)
}

// Implements sort.Interface
func (l noticeList) Less(i, j int) bool {
	return strings.Compare(l[i].promptID, l[j].promptID) < 0
}

// Implements sort.Interface
func (l noticeList) Swap(i, j int) {
	atI := l[i]
	l[i] = l[j]
	l[j] = atI
}

type requestpromptsSuite struct {
	defaultNotifyPrompt func(userID uint32, promptID string, data map[string]string) error
	defaultUser         uint32
	promptNotices       noticeList

	tmpdir string
}

var _ = Suite(&requestpromptsSuite{})

func (s *requestpromptsSuite) SetUpTest(c *C) {
	s.defaultUser = 1000
	s.defaultNotifyPrompt = func(userID uint32, promptID string, data map[string]string) error {
		c.Check(userID, Equals, s.defaultUser)
		info := &noticeInfo{
			promptID: promptID,
			data:     data,
		}
		s.promptNotices = append(s.promptNotices, info)
		return nil
	}
	s.promptNotices = make([]*noticeInfo, 0)
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0700), IsNil)
}

func (s *requestpromptsSuite) TestNew(c *C) {
	notifyPrompt := func(userID uint32, promptID string, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %s", userID, promptID)
		return nil
	}
	pdb := requestprompts.New(notifyPrompt)
	c.Check(pdb.PerUser(), HasLen, 0)
	c.Check(pdb.MaxID(), Equals, uint64(0))
}

func (s *requestpromptsSuite) TestLoadMaxID(c *C) {
	notifyPrompt := func(userID uint32, promptID string, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %s", userID, promptID)
		return nil
	}
	for _, testCase := range []struct {
		fileContents []byte
		initialMaxID uint64
	}{
		{
			[]byte("0000000000000000"),
			0,
		},
		{
			[]byte("0000000000000001"),
			1,
		},
		{
			[]byte("1000000000000001"),
			0x1000000000000001,
		},
		{
			[]byte("1234"),
			0,
		},
		{
			[]byte("deadbeefdeadbeef"),
			0xdeadbeefdeadbeef,
		},
		{
			[]byte("deadbeef"),
			0,
		},
		{
			[]byte("foo"),
			0,
		},
		{
			[]byte("foobarbazqux1234"),
			0,
		},
	} {
		osutil.AtomicWriteFile(filepath.Join(dirs.SnapRunDir, "/request-prompt-max-id"), testCase.fileContents, 0600, 0)
		pdb := requestprompts.New(notifyPrompt)
		c.Check(pdb.MaxID(), Equals, testCase.initialMaxID)
	}
}

func (s *requestpromptsSuite) TestLoadMaxIDNextID(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	var prevMaxID uint64 = 42
	maxIDStr := fmt.Sprintf("%016X", prevMaxID)
	osutil.AtomicWriteFile(filepath.Join(dirs.SnapRunDir, "/request-prompt-max-id"), []byte(maxIDStr), 0600, 0)

	pdb1 := requestprompts.New(s.defaultNotifyPrompt)
	c.Check(pdb1.PerUser(), HasLen, 0)
	c.Check(pdb1.MaxID(), Equals, prevMaxID)

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}
	prompt, merged := pdb1.AddOrMerge(metadata, path, permissions, listenerReq)
	c.Assert(merged, Equals, false)
	s.checkWrittenMaxID(c, prompt.ID)

	expectedID := prevMaxID + 1
	expectedIDStr := fmt.Sprintf("%016X", expectedID)
	s.checkWrittenMaxID(c, expectedIDStr)

	pdb2 := requestprompts.New(s.defaultNotifyPrompt)
	// New prompt DB should not have existing prompts, but should start from previous max ID
	c.Check(pdb2.PerUser(), HasLen, 0)
	c.Check(pdb1.MaxID(), Equals, prevMaxID+1)
}

func (s *requestpromptsSuite) checkWrittenMaxID(c *C, id string) {
	maxIDPath := filepath.Join(dirs.SnapRunDir, "/request-prompt-max-id")
	data, err := os.ReadFile(maxIDPath)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, id)
}

func (s *requestpromptsSuite) TestAddOrMerge(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq1 := &listener.Request{}
	listenerReq2 := &listener.Request{}
	listenerReq3 := &listener.Request{}

	stored := pdb.Prompts(metadata.User)
	c.Assert(stored, HasLen, 0)

	before := time.Now()
	prompt1, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq1)
	after := time.Now()
	c.Assert(merged, Equals, false)

	s.checkNewNoticesSimple(c, []string{prompt1.ID}, nil)
	c.Check(pdb.MaxID(), Equals, uint64(1))
	s.checkWrittenMaxID(c, prompt1.ID)

	prompt2, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq2)
	c.Assert(merged, Equals, true)
	c.Assert(prompt2, Equals, prompt1)

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []string{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	c.Check(pdb.MaxID(), Equals, uint64(1))
	s.checkWrittenMaxID(c, prompt1.ID)

	c.Check(prompt1.Timestamp.After(before), Equals, true)
	c.Check(prompt1.Timestamp.Before(after), Equals, true)

	c.Check(prompt1.Snap, Equals, metadata.Snap)
	c.Check(prompt1.Interface, Equals, metadata.Interface)
	c.Check(prompt1.Constraints.Path, Equals, path)
	c.Check(prompt1.Constraints.Permissions, DeepEquals, permissions)

	stored = pdb.Prompts(metadata.User)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)

	storedPrompt, err := pdb.PromptWithID(metadata.User, prompt1.ID)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt1)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []string{}, nil)

	prompt3, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq3)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []string{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	c.Check(pdb.MaxID(), Equals, uint64(1))
	s.checkWrittenMaxID(c, prompt1.ID)
}

func (s *requestpromptsSuite) checkNewNoticesSimple(c *C, expectedPromptIDs []string, expectedData map[string]string) {
	s.checkNewNotices(c, applyNotices(expectedPromptIDs, expectedData))
}

func applyNotices(expectedPromptIDs []string, expectedData map[string]string) noticeList {
	expectedNotices := make(noticeList, len(expectedPromptIDs))
	for i, id := range expectedPromptIDs {
		info := &noticeInfo{
			promptID: id,
			data:     expectedData,
		}
		expectedNotices[i] = info
	}
	return expectedNotices
}

func (s *requestpromptsSuite) checkNewNotices(c *C, expectedNotices noticeList) {
	c.Check(s.promptNotices, DeepEquals, expectedNotices)
	s.promptNotices = s.promptNotices[:0]
}

func (s *requestpromptsSuite) checkNewNoticesUnorderedSimple(c *C, expectedPromptIDs []string, expectedData map[string]string) {
	s.checkNewNoticesUnordered(c, applyNotices(expectedPromptIDs, expectedData))
}

func (s *requestpromptsSuite) checkNewNoticesUnordered(c *C, expectedNotices noticeList) {
	sort.Sort(s.promptNotices)
	sort.Sort(expectedNotices)
	s.checkNewNotices(c, expectedNotices)
}

func (s *requestpromptsSuite) TestPromptWithIDErrors(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}

	prompt, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []string{prompt.ID}, nil)

	result, err := pdb.PromptWithID(metadata.User, prompt.ID)
	c.Check(err, IsNil)
	c.Check(result, Equals, prompt)

	result, err = pdb.PromptWithID(metadata.User, "foo")
	c.Check(err, Equals, requestprompts.ErrNotFound)
	c.Check(result, IsNil)

	result, err = pdb.PromptWithID(metadata.User+1, "foo")
	c.Check(err, Equals, requestprompts.ErrNotFound)
	c.Check(result, IsNil)

	// Looking up prompts (with or without errors) should not record notices
	s.checkNewNoticesSimple(c, []string{}, nil)
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

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	for _, outcome := range []prompting.OutcomeType{prompting.OutcomeAllow, prompting.OutcomeDeny} {
		listenerReq1 := &listener.Request{}
		listenerReq2 := &listener.Request{}

		prompt1, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq1)
		c.Check(merged, Equals, false)

		s.checkNewNoticesSimple(c, []string{prompt1.ID}, nil)

		prompt2, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq2)
		c.Check(merged, Equals, true)
		c.Check(prompt2, Equals, prompt1)

		// Merged prompts should re-record notice
		s.checkNewNoticesSimple(c, []string{prompt1.ID}, nil)

		repliedPrompt, err := pdb.Reply(metadata.User, prompt1.ID, outcome)
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

		expectedData := map[string]string{"resolved": "replied"}
		s.checkNewNoticesSimple(c, []string{repliedPrompt.ID}, expectedData)
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

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "removable-media",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}

	prompt, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []string{prompt.ID}, nil)

	outcome := prompting.OutcomeAllow

	_, err := pdb.Reply(metadata.User, "foo", outcome)
	c.Check(err, Equals, requestprompts.ErrNotFound)

	_, err = pdb.Reply(metadata.User+1, "foo", outcome)
	c.Check(err, Equals, requestprompts.ErrNotFound)

	_, err = pdb.Reply(metadata.User, prompt.ID, prompting.OutcomeUnset)
	c.Check(err, ErrorMatches, `internal error: invalid outcome.*`)

	_, err = pdb.Reply(metadata.User, prompt.ID, outcome)
	c.Check(err, Equals, fakeError)

	// Failed replies should not record notice
	s.checkNewNoticesSimple(c, []string{}, nil)
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

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"

	permissions := []string{"read", "write", "execute"}
	listenerReq1 := &listener.Request{}
	prompt1, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []string{"read", "write"}
	listenerReq2 := &listener.Request{}
	prompt2, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []string{"read"}
	listenerReq3 := &listener.Request{}
	prompt3, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []string{"open"}
	listenerReq4 := &listener.Request{}
	prompt4, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []string{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID}, nil)

	stored := pdb.Prompts(metadata.User)
	c.Assert(stored, HasLen, 4)

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	permissions = []string{"read", "write", "append"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	outcome := prompting.OutcomeAllow

	satisfied, err := pdb.HandleNewRule(metadata, constraints, outcome)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(strutil.ListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt3.ID), Equals, true)

	// Read and write permissions of prompt1 satisfied, so notice re-issued,
	// but it has one remaining permission. prompt2 and prompt3 fully satisfied.
	e1 := &noticeInfo{promptID: prompt1.ID, data: nil}
	e2 := &noticeInfo{promptID: prompt2.ID, data: map[string]string{"resolved": "satisfied"}}
	e3 := &noticeInfo{promptID: prompt3.ID, data: map[string]string{"resolved": "satisfied"}}
	expectedNotices := noticeList{e1, e2, e3}
	s.checkNewNoticesUnordered(c, expectedNotices)

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

	stored = pdb.Prompts(metadata.User)
	c.Assert(stored, HasLen, 2)

	// Check that allowing the final missing permission allows the prompt.
	permissions = []string{"execute"}
	constraints = &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	satisfied, err = pdb.HandleNewRule(metadata, constraints, outcome)

	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 1)
	c.Check(satisfied[0], Equals, prompt1.ID)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesSimple(c, []string{prompt1.ID}, expectedData)

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

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"

	permissions := []string{"read", "write", "execute"}
	listenerReq1 := &listener.Request{}
	prompt1, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq1)
	c.Check(merged, Equals, false)

	permissions = []string{"read", "write"}
	listenerReq2 := &listener.Request{}
	prompt2, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq2)
	c.Check(merged, Equals, false)

	permissions = []string{"read"}
	listenerReq3 := &listener.Request{}
	prompt3, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq3)
	c.Check(merged, Equals, false)

	permissions = []string{"open"}
	listenerReq4 := &listener.Request{}
	prompt4, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq4)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []string{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID}, nil)

	stored := pdb.Prompts(metadata.User)
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
	satisfied, err := pdb.HandleNewRule(metadata, constraints, outcome)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 3)
	c.Check(strutil.ListContains(satisfied, prompt1.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(strutil.ListContains(satisfied, prompt3.ID), Equals, true)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesUnorderedSimple(c, []string{prompt1.ID, prompt2.ID, prompt3.ID}, expectedData)

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

	stored = pdb.Prompts(metadata.User)
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
	metadata := &prompting.Metadata{
		User:      user,
		Snap:      snap,
		Interface: iface,
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read"}
	listenerReq := &listener.Request{}
	prompt, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []string{prompt.ID}, nil)

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

	stored := pdb.Prompts(metadata.User)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], Equals, prompt)

	satisfied, err := pdb.HandleNewRule(metadata, constraints, badOutcome)
	c.Check(err, ErrorMatches, `internal error: invalid outcome.*`)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNoticesSimple(c, []string{}, nil)

	otherUserMetadata := &prompting.Metadata{
		User:      otherUser,
		Snap:      snap,
		Interface: iface,
	}
	satisfied, err = pdb.HandleNewRule(otherUserMetadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNoticesSimple(c, []string{}, nil)

	otherSnapMetadata := &prompting.Metadata{
		User:      user,
		Snap:      otherSnap,
		Interface: iface,
	}
	satisfied, err = pdb.HandleNewRule(otherSnapMetadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNoticesSimple(c, []string{}, nil)

	otherInterfaceMetadata := &prompting.Metadata{
		User:      user,
		Snap:      snap,
		Interface: otherInterface,
	}
	satisfied, err = pdb.HandleNewRule(otherInterfaceMetadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNoticesSimple(c, []string{}, nil)

	satisfied, err = pdb.HandleNewRule(metadata, otherConstraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	s.checkNewNoticesSimple(c, []string{}, nil)

	satisfied, err = pdb.HandleNewRule(metadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesSimple(c, []string{prompt.ID}, expectedData)

	satisfiedReq, result, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
	c.Check(err, IsNil)
	c.Check(satisfiedReq, Equals, listenerReq)
	allowed, ok := result.(bool)
	c.Check(ok, Equals, true)
	c.Check(allowed, Equals, true)

	stored = pdb.Prompts(metadata.User)
	c.Check(stored, HasLen, 0)
}

func (s *requestpromptsSuite) TestClose(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, reply interface{}) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb := requestprompts.New(s.defaultNotifyPrompt)

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	permissions := []string{"read", "write", "execute"}

	paths := []string{
		"/home/test/1.txt",
		"/home/test/2.txt",
		"/home/test/3.txt",
	}

	prompts := make([]*requestprompts.Prompt, 0, 3)
	for _, path := range paths {
		listenerReq := &listener.Request{}
		prompt, merged := pdb.AddOrMerge(metadata, path, permissions, listenerReq)
		c.Assert(merged, Equals, false)
		prompts = append(prompts, prompt)
	}

	expectedPromptIDs := make([]string, 0, 3)
	for _, prompt := range prompts {
		expectedPromptIDs = append(expectedPromptIDs, prompt.ID)
	}
	c.Check(pdb.MaxID(), Equals, uint64(3))

	// One notice for each prompt when created
	s.checkNewNoticesSimple(c, expectedPromptIDs, nil)

	pdb.Close()

	// Once notice for each prompt when cleaned up
	expectedData := map[string]string{"resolved": "cancelled"}
	s.checkNewNoticesUnorderedSimple(c, expectedPromptIDs, expectedData)

	// All prompts have been cleared, and all per-user maps deleted
	c.Check(pdb.PerUser(), HasLen, 0)
}
