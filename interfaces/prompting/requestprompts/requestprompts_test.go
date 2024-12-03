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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
	"unsafe"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testtime"
	"github.com/snapcore/snapd/timeutil"
)

func Test(t *testing.T) { TestingT(t) }

type noticeInfo struct {
	promptID prompting.IDType
	data     map[string]string
}

type requestpromptsSuite struct {
	defaultNotifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error
	defaultUser         uint32
	promptNotices       []*noticeInfo

	tmpdir    string
	maxIDPath string
}

var _ = Suite(&requestpromptsSuite{})

func (s *requestpromptsSuite) SetUpTest(c *C) {
	s.defaultUser = 1000
	s.defaultNotifyPrompt = func(userID uint32, promptID prompting.IDType, data map[string]string) error {
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
	s.maxIDPath = filepath.Join(dirs.SnapRunDir, "request-prompt-max-id")
}

func (s *requestpromptsSuite) TestNew(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %016X", userID, promptID)
		return nil
	}
	pdb, err := requestprompts.New(notifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()
	c.Check(pdb.PerUser(), HasLen, 0)
	nextID, err := pdb.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(1))
}

func (s *requestpromptsSuite) TestNewValidMaxID(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %016X", userID, promptID)
		return nil
	}
	for _, testCase := range []struct {
		initial uint64
		nextID  prompting.IDType
	}{
		{
			0,
			1,
		},
		{
			1,
			2,
		},
		{
			0x1000000000000001,
			0x1000000000000002,
		},
		{
			0x0123456789ABCDEF,
			0x0123456789ABCDF0,
		},
		{
			0xDEADBEEFDEADBEEF,
			0xDEADBEEFDEADBEF0,
		},
	} {
		var initialData [8]byte
		*(*uint64)(unsafe.Pointer(&initialData[0])) = testCase.initial
		osutil.AtomicWriteFile(s.maxIDPath, initialData[:], 0600, 0)
		pdb, err := requestprompts.New(notifyPrompt)
		c.Assert(err, IsNil)
		defer pdb.Close()
		s.checkWrittenMaxID(c, testCase.initial)
		nextID, err := pdb.NextID()
		c.Check(err, IsNil)
		c.Check(nextID, Equals, testCase.nextID)
		s.checkWrittenMaxID(c, testCase.initial+1)
	}
}

func (s *requestpromptsSuite) TestNewInvalidMaxID(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %016X", userID, promptID)
		return nil
	}

	// First try with no existing max ID file
	pdb, err := requestprompts.New(notifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()
	s.checkWrittenMaxID(c, 0)
	nextID, err := pdb.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(1))
	s.checkWrittenMaxID(c, 1)

	// Now try with various invalid max ID files
	for _, initial := range [][]byte{
		[]byte(""),
		[]byte("foo"),
		[]byte("1234"),
		[]byte("1234567"),
		[]byte("123456789"),
	} {
		osutil.AtomicWriteFile(s.maxIDPath, initial, 0600, 0)
		pdb, err := requestprompts.New(notifyPrompt)
		c.Assert(err, IsNil)
		defer pdb.Close()
		s.checkWrittenMaxID(c, 0)
		nextID, err := pdb.NextID()
		c.Check(err, IsNil)
		c.Check(nextID, Equals, prompting.IDType(1))
		s.checkWrittenMaxID(c, 1)
	}
}

func (s *requestpromptsSuite) TestNewNextIDUniqueIDs(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0o755), IsNil)

	var initialMaxID uint64 = 42
	var initialData [8]byte
	*(*uint64)(unsafe.Pointer(&initialData[0])) = initialMaxID
	osutil.AtomicWriteFile(s.maxIDPath, initialData[:], 0600, 0)

	pdb1, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb1.Close()
	expectedID := initialMaxID + 1
	nextID, err := pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))
	s.checkWrittenMaxID(c, expectedID)

	// New prompt DB should start where existing one left off
	pdb2, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb2.Close()
	expectedID++
	nextID, err = pdb2.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	// Both prompt DBs should be aware of any new IDs created by any others
	expectedID++
	nextID, err = pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	expectedID++
	nextID, err = pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	expectedID++
	nextID, err = pdb2.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	// For the checks above to have passed, incremented IDs must have been
	// written to disk, but check now anyway. Theoretically, checking disk
	// earlier might have caused mmaped data to be flushed, so wait until now.
	s.checkWrittenMaxID(c, expectedID)
}

func (s *requestpromptsSuite) checkWrittenMaxID(c *C, id uint64) {
	data, err := os.ReadFile(s.maxIDPath)
	c.Assert(err, IsNil)
	c.Assert(data, HasLen, 8)
	writtenID := *(*uint64)(unsafe.Pointer(&data[0]))
	c.Assert(writtenID, Equals, id)
}

func (s *requestpromptsSuite) TestAddOrMerge(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

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

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, IsNil)

	before := time.Now()
	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq1)
	c.Assert(err, IsNil)
	after := time.Now()
	c.Assert(merged, Equals, false)

	expectedID := uint64(1)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	s.checkWrittenMaxID(c, expectedID)

	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq2)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, true)
	c.Assert(prompt2, Equals, prompt1)

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	s.checkWrittenMaxID(c, expectedID)

	c.Check(prompt1.Timestamp.After(before), Equals, true)
	c.Check(prompt1.Timestamp.Before(after), Equals, true)

	c.Check(prompt1.Snap, Equals, metadata.Snap)
	c.Check(prompt1.Interface, Equals, metadata.Interface)
	c.Check(prompt1.Constraints.Path(), Equals, path)
	c.Check(prompt1.Constraints.RemainingPermissions(), DeepEquals, permissions)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)

	storedPrompt, err := pdb.PromptWithID(metadata.User, prompt1.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt1)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	s.checkWrittenMaxID(c, expectedID)
}

func (s *requestpromptsSuite) checkNewNoticesSimple(c *C, expectedPromptIDs []prompting.IDType, expectedData map[string]string) {
	s.checkNewNotices(c, applyNotices(expectedPromptIDs, expectedData))
}

func applyNotices(expectedPromptIDs []prompting.IDType, expectedData map[string]string) []*noticeInfo {
	expectedNotices := make([]*noticeInfo, len(expectedPromptIDs))
	for i, id := range expectedPromptIDs {
		info := &noticeInfo{
			promptID: id,
			data:     expectedData,
		}
		expectedNotices[i] = info
	}
	return expectedNotices
}

func (s *requestpromptsSuite) checkNewNotices(c *C, expectedNotices []*noticeInfo) {
	c.Check(s.promptNotices, DeepEquals, expectedNotices)
	s.promptNotices = s.promptNotices[:0]
}

func (s *requestpromptsSuite) checkNewNoticesUnorderedSimple(c *C, expectedPromptIDs []prompting.IDType, expectedData map[string]string) {
	s.checkNewNoticesUnordered(c, applyNotices(expectedPromptIDs, expectedData))
}

func (s *requestpromptsSuite) checkNewNoticesUnordered(c *C, expectedNotices []*noticeInfo) {
	sort.Slice(sortSliceParams(s.promptNotices))
	sort.Slice(sortSliceParams(expectedNotices))
	s.checkNewNotices(c, expectedNotices)
}

func sortSliceParams(list []*noticeInfo) ([]*noticeInfo, func(i, j int) bool) {
	less := func(i, j int) bool {
		return list[i].promptID < list[j].promptID
	}
	return list, less
}

func (s *requestpromptsSuite) TestAddOrMergeTooMany(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}

	permissions := []string{"read", "write", "execute"}
	clientActivity := false // doesn't matter if it's true or false for this test

	for i := 0; i < requestprompts.MaxOutstandingPromptsPerUser; i++ {
		path := fmt.Sprintf("/home/test/Documents/%d.txt", i)
		listenerReq := &listener.Request{}
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq)
		c.Assert(err, IsNil)
		c.Assert(prompt, Not(IsNil))
		c.Assert(merged, Equals, false)
		stored, err := pdb.Prompts(metadata.User, clientActivity)
		c.Assert(err, IsNil)
		c.Assert(stored, HasLen, i+1)
	}

	path := fmt.Sprintf("/home/test/Documents/%d.txt", requestprompts.MaxOutstandingPromptsPerUser)
	lr := &listener.Request{}

	restore = requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Assert(listenerReq, Equals, lr)
		c.Assert(allowedPermission, DeepEquals, notify.FilePermission(0))
		return nil
	})
	defer restore()

	// Check that adding a new unmerged prompt fails once limit is reached
	for i := 0; i < 5; i++ {
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, lr)
		c.Check(err, Equals, prompting_errors.ErrTooManyPrompts)
		c.Check(prompt, IsNil)
		c.Check(merged, Equals, false)
		stored, err := pdb.Prompts(metadata.User, clientActivity)
		c.Assert(err, IsNil)
		c.Assert(stored, HasLen, requestprompts.MaxOutstandingPromptsPerUser)
	}

	// Restore sendReply to fail if called
	restore()

	// Check that new requests can still merge into existing prompts
	for i := 0; i < requestprompts.MaxOutstandingPromptsPerUser; i++ {
		path := fmt.Sprintf("/home/test/Documents/%d.txt", i)
		listenerReq := &listener.Request{}
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq)
		c.Assert(err, IsNil)
		c.Assert(prompt, Not(IsNil))
		c.Assert(merged, Equals, true)
		stored, err := pdb.Prompts(metadata.User, clientActivity)
		c.Assert(err, IsNil)
		// Number of stored prompts remains the maximum
		c.Assert(stored, HasLen, requestprompts.MaxOutstandingPromptsPerUser)
	}
}

func (s *requestpromptsSuite) TestPromptWithIDErrors(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}

	prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, nil)

	clientActivity := true // doesn't matter if it's true or false for this test
	result, err := pdb.PromptWithID(metadata.User, prompt.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(result, Equals, prompt)

	result, err = pdb.PromptWithID(metadata.User, 1234, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	c.Check(result, IsNil)

	result, err = pdb.PromptWithID(metadata.User+1, prompt.ID, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	c.Check(result, IsNil)

	// Looking up prompts (with or without errors) should not record notices
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}

func (s *requestpromptsSuite) TestReply(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan any, 2)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		listenerReqChan <- listenerReq
		replyChan <- allowedPermission
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

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

		prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq1)
		c.Assert(err, IsNil)
		c.Check(merged, Equals, false)

		s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)

		prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq2)
		c.Assert(err, IsNil)
		c.Check(merged, Equals, true)
		c.Check(prompt2, Equals, prompt1)

		// Merged prompts should re-record notice
		s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)

		clientActivity := true // doesn't matter if it's true or false for this test
		repliedPrompt, err := pdb.Reply(metadata.User, prompt1.ID, outcome, clientActivity)
		c.Check(err, IsNil)
		c.Check(repliedPrompt, Equals, prompt1)
		for _, listenerReq := range []*listener.Request{listenerReq1, listenerReq2} {
			receivedReq, allowedPermission, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
			c.Check(err, IsNil)
			c.Check(receivedReq, Equals, listenerReq)
			allow, err := outcome.AsBool()
			c.Check(err, IsNil)
			if allow {
				// Check that permissions in response map to prompt's permissions
				abstractPermissions, err := prompting.AbstractPermissionsFromAppArmorPermissions(prompt1.Interface, allowedPermission)
				c.Check(err, IsNil)
				c.Check(abstractPermissions, DeepEquals, prompt1.Constraints.RemainingPermissions())
				// Check that prompt's permissions map to response's permissions
				expectedPerm, err := prompting.AbstractPermissionsToAppArmorPermissions(prompt1.Interface, prompt1.Constraints.RemainingPermissions())
				c.Check(err, IsNil)
				c.Check(allowedPermission, DeepEquals, expectedPerm)
			} else {
				// Check that no permissions were allowed
				c.Check(allowedPermission, DeepEquals, notify.FilePermission(0))
			}
		}

		expectedData := map[string]string{"resolved": "replied"}
		s.checkNewNoticesSimple(c, []prompting.IDType{repliedPrompt.ID}, expectedData)
	}
}

func (s *requestpromptsSuite) waitForListenerReqAndReply(c *C, listenerReqChan <-chan *listener.Request, replyChan <-chan any) (req *listener.Request, allowedPermission any, err error) {
	select {
	case req = <-listenerReqChan:
	case <-time.NewTimer(10 * time.Second).C:
		err = fmt.Errorf("failed to receive request over channel")
	}
	select {
	case allowedPermission = <-replyChan:
	case <-time.NewTimer(10 * time.Second).C:
		err = fmt.Errorf("failed to receive reply over channel")
	}
	return req, allowedPermission, err
}

func (s *requestpromptsSuite) TestReplyErrors(c *C) {
	fakeError := fmt.Errorf("fake reply error")
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		return fakeError
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	listenerReq := &listener.Request{}

	prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, nil)

	outcome := prompting.OutcomeAllow

	clientActivity := true // doesn't matter if it's true or false for this test
	_, err = pdb.Reply(metadata.User, 1234, outcome, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)

	_, err = pdb.Reply(metadata.User+1, prompt.ID, outcome, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)

	_, err = pdb.Reply(metadata.User, prompt.ID, outcome, clientActivity)
	c.Check(err, Equals, fakeError)

	// Failed replies should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}

func (s *requestpromptsSuite) TestHandleNewRuleAllowPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 2)
	replyChan := make(chan any, 2)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		listenerReqChan <- listenerReq
		replyChan <- allowedPermission
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"

	permissions1 := []string{"read", "write", "execute"}
	listenerReq1 := &listener.Request{}
	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions1, permissions1, listenerReq1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions2 := []string{"read", "write"}
	listenerReq2 := &listener.Request{}
	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions2, permissions2, listenerReq2)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions3 := []string{"read"}
	listenerReq3 := &listener.Request{}
	prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions3, permissions3, listenerReq3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions4 := []string{"open"}
	listenerReq4 := &listener.Request{}
	prompt4, merged, err := pdb.AddOrMerge(metadata, path, permissions4, permissions4, listenerReq4)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID}, nil)

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 4)

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	permissions := []string{"read", "write", "append"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	outcome := prompting.OutcomeAllow

	satisfied, err := pdb.HandleNewRule(metadata, constraints, outcome)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(promptIDListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(promptIDListContains(satisfied, prompt3.ID), Equals, true)

	// Read and write permissions of prompt1 satisfied, so notice re-issued,
	// but it has one remaining permission. prompt2 and prompt3 fully satisfied.
	e1 := &noticeInfo{promptID: prompt1.ID, data: nil}
	e2 := &noticeInfo{promptID: prompt2.ID, data: map[string]string{"resolved": "satisfied"}}
	e3 := &noticeInfo{promptID: prompt3.ID, data: map[string]string{"resolved": "satisfied"}}
	expectedNotices := []*noticeInfo{e1, e2, e3}
	s.checkNewNoticesUnordered(c, expectedNotices)

	for i := 0; i < 2; i++ {
		satisfiedReq, allowedPermission, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
		c.Check(err, IsNil)
		var perms []string
		switch satisfiedReq {
		case listenerReq2:
			perms = permissions2
		case listenerReq3:
			perms = permissions3
		default:
			c.Errorf("unexpected request satisfied by new rule")
		}
		expectedPerm, err := prompting.AbstractPermissionsToAppArmorPermissions(metadata.Interface, perms)
		c.Check(err, IsNil)
		c.Check(allowedPermission, DeepEquals, expectedPerm)
	}

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
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
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, expectedData)

	satisfiedReq, allowedPermission, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
	c.Check(err, IsNil)
	c.Check(satisfiedReq, Equals, listenerReq1)
	expectedPerm, err := prompting.AbstractPermissionsToAppArmorPermissions(metadata.Interface, permissions1)
	c.Check(err, IsNil)
	c.Check(allowedPermission, DeepEquals, expectedPerm)
}

func promptIDListContains(haystack []prompting.IDType, needle prompting.IDType) bool {
	for _, id := range haystack {
		if id == needle {
			return true
		}
	}
	return false
}

func (s *requestpromptsSuite) TestHandleNewRuleDenyPermissions(c *C) {
	listenerReqChan := make(chan *listener.Request, 3)
	replyChan := make(chan any, 3)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		listenerReqChan <- listenerReq
		replyChan <- allowedPermission
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"

	permissions1 := []string{"read", "write", "execute"}
	listenerReq1 := &listener.Request{}
	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions1, permissions1, listenerReq1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions2 := []string{"read", "write"}
	listenerReq2 := &listener.Request{}
	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions2, permissions2, listenerReq2)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions3 := []string{"read"}
	listenerReq3 := &listener.Request{}
	prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions3, permissions3, listenerReq3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions4 := []string{"open"}
	listenerReq4 := &listener.Request{}
	prompt4, merged, err := pdb.AddOrMerge(metadata, path, permissions4, permissions4, listenerReq4)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID}, nil)

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 4)

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	permissions := []string{"read"}
	constraints := &prompting.Constraints{
		PathPattern: pathPattern,
		Permissions: permissions,
	}
	outcome := prompting.OutcomeDeny

	// If one or more permissions denied each for prompts 1-3, so each is denied
	satisfied, err := pdb.HandleNewRule(metadata, constraints, outcome)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 3)
	c.Check(promptIDListContains(satisfied, prompt1.ID), Equals, true)
	c.Check(promptIDListContains(satisfied, prompt2.ID), Equals, true)
	c.Check(promptIDListContains(satisfied, prompt3.ID), Equals, true)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesUnorderedSimple(c, []prompting.IDType{prompt1.ID, prompt2.ID, prompt3.ID}, expectedData)

	for i := 0; i < 3; i++ {
		satisfiedReq, allowedPermission, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
		c.Check(err, IsNil)
		switch satisfiedReq {
		case listenerReq1, listenerReq2, listenerReq3:
			break
		default:
			c.Errorf("unexpected request satisfied by new rule")
		}
		c.Check(allowedPermission, DeepEquals, notify.FilePermission(0))
	}

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Check(err, IsNil)
	c.Check(stored, HasLen, 1)
}

func (s *requestpromptsSuite) TestHandleNewRuleNonMatches(c *C) {
	listenerReqChan := make(chan *listener.Request, 1)
	replyChan := make(chan any, 1)
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		listenerReqChan <- listenerReq
		replyChan <- allowedPermission
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

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
	prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, nil)

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

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], Equals, prompt)

	satisfied, err := pdb.HandleNewRule(metadata, constraints, badOutcome)
	c.Check(err, ErrorMatches, `invalid outcome: "foo"`)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	otherUserMetadata := &prompting.Metadata{
		User:      otherUser,
		Snap:      snap,
		Interface: iface,
	}
	satisfied, err = pdb.HandleNewRule(otherUserMetadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	otherSnapMetadata := &prompting.Metadata{
		User:      user,
		Snap:      otherSnap,
		Interface: iface,
	}
	satisfied, err = pdb.HandleNewRule(otherSnapMetadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	otherInterfaceMetadata := &prompting.Metadata{
		User:      user,
		Snap:      snap,
		Interface: otherInterface,
	}
	satisfied, err = pdb.HandleNewRule(otherInterfaceMetadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	satisfied, err = pdb.HandleNewRule(metadata, otherConstraints, outcome)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	satisfied, err = pdb.HandleNewRule(metadata, constraints, outcome)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, expectedData)

	satisfiedReq, allowedPermission, err := s.waitForListenerReqAndReply(c, listenerReqChan, replyChan)
	c.Check(err, IsNil)
	c.Check(satisfiedReq, Equals, listenerReq)
	expectedPerm, err := prompting.AbstractPermissionsToAppArmorPermissions(metadata.Interface, permissions)
	c.Check(err, IsNil)
	c.Check(allowedPermission, DeepEquals, expectedPerm)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Check(err, IsNil)
	c.Check(stored, IsNil)
}

func (s *requestpromptsSuite) TestClose(c *C) {
	var timer *testtime.TestTimer
	restore := requestprompts.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, f)
		return timer
	})
	defer restore()

	restore = requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)

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
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, listenerReq)
		c.Assert(err, IsNil)
		c.Assert(merged, Equals, false)
		prompts = append(prompts, prompt)
	}

	expectedPromptIDs := make([]prompting.IDType, 0, 3)
	for _, prompt := range prompts {
		expectedPromptIDs = append(expectedPromptIDs, prompt.ID)
	}
	c.Check(prompts[2].ID, Equals, prompting.IDType(3))

	// One notice for each prompt when created
	s.checkNewNoticesSimple(c, expectedPromptIDs, nil)

	pdb.Close()

	// Once notice for each prompt when cleaned up
	expectedData := map[string]string{"resolved": "cancelled"}
	s.checkNewNoticesUnorderedSimple(c, expectedPromptIDs, expectedData)

	// All prompts have been cleared, and all per-user maps deleted
	c.Check(pdb.PerUser(), HasLen, 0)

	// Sense check that the timer is still active, though this is not part of
	// any contract, and there's no reason that closing the timer shouldn't be
	// allowed to stop the expiration timers. We don't at the moment because
	// doing so is racy and unnecessary, though there's no harm in closing them.
	c.Check(timer.Active(), Equals, true)

	// Elapse time as if the prompt timer expired
	timer.Elapse(requestprompts.InitialTimeout + requestprompts.ActivityTimeout)
	// Check that timer expiration did not result in new notices
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
	// Check that the timer is no longer active. Since the DB is closed, the
	// expiration callback should not reset the timer as it usually would.
	c.Check(timer.Active(), Equals, false)
}

func (s *requestpromptsSuite) TestCloseThenOperate(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)

	err = pdb.Close()
	c.Assert(err, IsNil)

	nextID, err := pdb.NextID()
	c.Check(err, Equals, maxidmmap.ErrMaxIDMmapClosed)
	c.Check(nextID, Equals, prompting.IDType(0))

	metadata := prompting.Metadata{Interface: "home"}
	result, merged, err := pdb.AddOrMerge(&metadata, "", nil, nil, nil)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(result, IsNil)
	c.Check(merged, Equals, false)

	clientActivity := false // doesn't matter if it's true or false for this test
	prompts, err := pdb.Prompts(1000, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(prompts, IsNil)

	prompt, err := pdb.PromptWithID(1000, 1, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(prompt, IsNil)

	result, err = pdb.Reply(1000, 1, prompting.OutcomeDeny, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(result, IsNil)

	promptIDs, err := pdb.HandleNewRule(nil, nil, prompting.OutcomeDeny)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(promptIDs, IsNil)

	err = pdb.Close()
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
}

func (s *requestpromptsSuite) TestPromptMarshalJSON(c *C) {
	restore := requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		c.Fatalf("should not have called sendReply")
		return nil
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "firefox",
		Interface: "home",
	}
	path := "/home/test/foo"
	requestedPermissions := []string{"read", "write", "execute"}
	remainingPermissions := []string{"write", "execute"}

	prompt, merged, err := pdb.AddOrMerge(metadata, path, requestedPermissions, remainingPermissions, nil)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)

	// Set timestamp to a known time
	timeStr := "2024-08-14T09:47:03.350324989-05:00"
	prompt.Timestamp, err = time.Parse(time.RFC3339Nano, timeStr)
	c.Assert(err, IsNil)

	expectedJSON := `{"id":"0000000000000001","timestamp":"2024-08-14T09:47:03.350324989-05:00","snap":"firefox","interface":"home","constraints":{"path":"/home/test/foo","requested-permissions":["write","execute"],"available-permissions":["read","write","execute"]}}`

	marshalled, err := json.Marshal(prompt)
	c.Assert(err, IsNil)

	c.Assert(string(marshalled), Equals, string(expectedJSON))
}

func (s *requestpromptsSuite) TestPromptExpiration(c *C) {
	var timer *testtime.TestTimer
	restore := requestprompts.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, f)
		return timer
	})
	defer restore()

	replyChan := make(chan notify.FilePermission, 1)
	restore = requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		allowedFilePermission, ok := allowedPermission.(notify.FilePermission)
		c.Assert(ok, Equals, true)
		replyChan <- allowedFilePermission
		return nil
	})
	defer restore()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "firefox",
		Interface: "home",
	}
	path := "/home/test/foo"
	requestedPermissions := []string{"read", "write", "execute"}
	remainingPermissions := []string{"write", "execute"}

	noticeChan := make(chan noticeInfo, 1)
	pdb, err := requestprompts.New(func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Assert(userID, Equals, s.defaultUser)
		noticeChan <- noticeInfo{
			promptID: promptID,
			data:     data,
		}
		return nil
	})
	c.Assert(err, IsNil)
	defer pdb.Close()

	// Add prompt
	listenerReq := &listener.Request{}
	prompt, merged, err := pdb.AddOrMerge(metadata, path, requestedPermissions, remainingPermissions, listenerReq)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)

	// Check that prompt has not immediately expired
	c.Assert(timer.FireCount(), Equals, 0)

	// Prompt should *not* expire after half of initialTimeout
	timer.Elapse(requestprompts.InitialTimeout / 2)
	c.Assert(timer.FireCount(), Equals, 0)

	// Add another prompt, check that it does not bump the activity timeout
	listenerReq = &listener.Request{}
	otherPath := "/home/test/bar"
	prompt2, merged, err := pdb.AddOrMerge(metadata, otherPath, requestedPermissions, remainingPermissions, listenerReq)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt2.ID, nil)

	// Prompt should expire after initialTimeout, but half already elapsed
	timer.Elapse(requestprompts.InitialTimeout - requestprompts.InitialTimeout/2)
	checkCurrentNoticesMultiple(c, noticeChan, []prompting.IDType{prompt.ID, prompt2.ID}, map[string]string{"resolved": "expired"})
	// Expect two replies, one for each prompt
	waitForReply(c, replyChan)
	waitForReply(c, replyChan)
	c.Assert(timer.FireCount(), Equals, 1)

	// Add prompt again
	listenerReq = &listener.Request{}
	prompt, merged, err = pdb.AddOrMerge(metadata, path, requestedPermissions, remainingPermissions, listenerReq)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)

	// Retrieve prompts for s.defaultUser, and bump timeout
	clientActivity := true
	prompts, err := pdb.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{prompt})

	// Prompt should *not* expire after initialTimeout (or even double it)
	timer.Elapse(2 * requestprompts.InitialTimeout)
	c.Assert(timer.FireCount(), Equals, 1)

	// Retrieve prompt by ID, and bump timeout
	p, err := pdb.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(p, Equals, prompt)

	// Prompt should *not* expire after activityTimeout-1ns
	timer.Elapse(requestprompts.ActivityTimeout - time.Nanosecond)
	c.Assert(timer.FireCount(), Equals, 1)

	// Reply to fake prompt (and get error, but still bump timeout)
	_, err = pdb.Reply(s.defaultUser, prompt.ID+1, prompting.OutcomeAllow, clientActivity)
	c.Check(err, NotNil)

	// Prompt should *not* expire after initialTimeout
	timer.Elapse(requestprompts.InitialTimeout)
	c.Assert(timer.FireCount(), Equals, 1)

	// Prompt should expire after activityTimeout
	timer.Elapse(requestprompts.ActivityTimeout - requestprompts.InitialTimeout)
	checkCurrentNotices(c, noticeChan, prompt.ID, map[string]string{"resolved": "expired"})
	waitForReply(c, replyChan)
	c.Assert(timer.FireCount(), Equals, 2)

	// Add prompt again
	listenerReq = &listener.Request{}
	prompt, merged, err = pdb.AddOrMerge(metadata, path, requestedPermissions, remainingPermissions, listenerReq)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)

	// Check that prompt has not immediately expired
	c.Assert(timer.FireCount(), Equals, 2)
	// Nor after initialTimeout-1ns
	timer.Elapse(requestprompts.InitialTimeout - time.Nanosecond)
	c.Assert(timer.FireCount(), Equals, 2)

	// Get prompts but do not bump timeout
	clientActivity = false
	prompts, err = pdb.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{prompt})

	// After timing out, timer should be reset to initialTimeout, rather than
	// activity timeout, so prompt should expire after initialTimeout (since we
	// already elapsed initialTimeout-1ns, just wait 1ns more).
	timer.Elapse(time.Nanosecond)
	checkCurrentNotices(c, noticeChan, prompt.ID, map[string]string{"resolved": "expired"})
	waitForReply(c, replyChan)
	c.Assert(timer.FireCount(), Equals, 3)
}

func (s *requestpromptsSuite) TestPromptExpirationRace(c *C) {
	callbackSignaller := make(chan bool, 0)
	var timer *testtime.TestTimer
	restore := requestprompts.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		callback := func() {
			// Wait for a signal over startCallback
			<-callbackSignaller
			f()
			callbackSignaller <- true
		}
		timer = testtime.AfterFunc(d, callback)
		return timer
	})
	defer restore()

	replyChan := make(chan notify.FilePermission, 1)
	restore = requestprompts.MockSendReply(func(listenerReq *listener.Request, allowedPermission any) error {
		allowedFilePermission, ok := allowedPermission.(notify.FilePermission)
		c.Assert(ok, Equals, true)
		replyChan <- allowedFilePermission
		return nil
	})
	defer restore()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "firefox",
		Interface: "home",
	}
	path := "/home/test/foo"
	requestedPermissions := []string{"read", "write", "execute"}
	remainingPermissions := []string{"write", "execute"}

	noticeChan := make(chan noticeInfo, 1)
	pdb, err := requestprompts.New(func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Assert(userID, Equals, s.defaultUser)
		noticeChan <- noticeInfo{
			promptID: promptID,
			data:     data,
		}
		return nil
	})
	c.Assert(err, IsNil)
	defer pdb.Close()

	// Add prompt
	listenerReq := &listener.Request{}
	prompt, merged, err := pdb.AddOrMerge(metadata, path, requestedPermissions, remainingPermissions, listenerReq)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)

	// Check that prompt has not immediately expired
	c.Assert(timer.FireCount(), Equals, 0)

	// Cause prompt to timeout, but the callback will wait for a signal, so we
	// can reset it, simulating activity occurring just as the timer fires.
	timer.Elapse(requestprompts.InitialTimeout)

	// Check that the timer fired
	c.Assert(timer.FireCount(), Equals, 1)

	// Reset timer to half of initial timeout, as if activity occurred, but it's
	// easier to check that the timeout was correctly reset to activityTimeout
	// if the preemptive reset was not also to activityTimeout.
	//
	// In the real world, what would have happened is that activity occurred
	// just as the timer fired, thus resetting the timer to activityTimeout
	// just before the timeout callback sets it to initialTimeout, and we want
	// to ensure that the callback correctly detects that the activity had
	// occurred (by the timer being active again) and overrides its own just-set
	// initialTimeout by resetting the timer back to activityTimeout.
	timer.Reset(requestprompts.InitialTimeout / 2)

	// Start the actual callback
	callbackSignaller <- true
	// Wait for the callback to complete
	<-callbackSignaller

	// Check that prompt has not expired
	clientActivity := false
	retrieved, err := pdb.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(retrieved, Equals, prompt)
	c.Assert(timer.FireCount(), Equals, 1)

	// Check that the callback correctly identified that the timer had been
	// reset prior to the callback doing so, and thus re-reset the timeout to
	// activityTimeout instead of leaving it reset to initialTimeout.
	// First, check that the prompt doesn't expire after the preemptively reset
	// timeout (but before initialTimeout).
	timer.Elapse(requestprompts.InitialTimeout - requestprompts.InitialTimeout/4)
	c.Assert(timer.FireCount(), Equals, 1)
	// Next, check that the prompt doesn't expire after the full initialTimeout
	// (but before activityTimeout).
	timer.Elapse(requestprompts.InitialTimeout / 4)
	c.Assert(timer.FireCount(), Equals, 1)

	// Check that the prompt does expire after the total activityTimeout
	// following a race while the timeout was firing
	timer.Elapse(requestprompts.ActivityTimeout - requestprompts.InitialTimeout)
	c.Assert(timer.FireCount(), Equals, 2)

	// Allow the callback to run
	callbackSignaller <- true
	// Wait for it to finish
	<-callbackSignaller

	checkCurrentNotices(c, noticeChan, prompt.ID, map[string]string{"resolved": "expired"})
	waitForReply(c, replyChan)

	_, err = pdb.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Assert(err, Equals, prompting_errors.ErrPromptNotFound)
}

func checkCurrentNotices(c *C, noticeChan chan noticeInfo, expectedID prompting.IDType, expectedData map[string]string) {
	select {
	case info := <-noticeChan:
		c.Assert(info.promptID, Equals, expectedID)
		c.Assert(info.data, DeepEquals, expectedData)
	case <-time.NewTimer(10 * time.Second).C:
		c.Fatal("no notices")
	}
}

func checkCurrentNoticesMultiple(c *C, noticeChan chan noticeInfo, expectedIDs []prompting.IDType, expectedData map[string]string) {
	expected := make(map[prompting.IDType]int)
	for _, id := range expectedIDs {
		expected[id] += 1
	}
	seen := make(map[prompting.IDType]int)
	for range expectedIDs {
		select {
		case info := <-noticeChan:
			seen[info.promptID] += 1
			c.Assert(info.data, DeepEquals, expectedData)
		case <-time.NewTimer(10 * time.Second).C:
			c.Fatal("no notices")
		}
	}
	c.Assert(seen, DeepEquals, expected)
}

func waitForReply(c *C, replyChan chan notify.FilePermission) {
	select {
	case allowedPermission := <-replyChan:
		// Allow all permissions mapping to "read" for the "home" interface,
		// which are read|getattr|getattr.
		c.Assert(allowedPermission, Equals, notify.AA_MAY_READ|notify.AA_MAY_OPEN|notify.AA_MAY_GETATTR)
	case <-time.NewTimer(10 * time.Second).C:
		c.Fatalf("timed out waiting for reply")
	}
}
