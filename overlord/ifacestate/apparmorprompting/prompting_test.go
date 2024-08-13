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

package apparmorprompting_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type apparmorpromptingSuite struct {
	testutil.BaseTest

	st *state.State

	defaultUser uint32
}

var _ = Suite(&apparmorpromptingSuite{})

func (s *apparmorpromptingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	os.MkdirAll(dirs.SnapRunDir, 0o755)

	s.st = state.New(nil)
	s.defaultUser = 1000
}

func (s *apparmorpromptingSuite) TestNew(c *C) {
	_, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	err = mgr.Stop()
	c.Assert(err, IsNil)
}

func (s *apparmorpromptingSuite) TestNewErrorListener(c *C) {
	registerFailure := fmt.Errorf("failed to register listener")
	restore := apparmorprompting.MockListenerRegister(func() (*listener.Listener, error) {
		return nil, registerFailure
	})
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot register request listener: %v", registerFailure))
	c.Assert(mgr, IsNil)
}

func (s *apparmorpromptingSuite) TestNewErrorPromptDB(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Prevent prompt backend from opening successfully
	maxIDFilepath := filepath.Join(dirs.SnapRunDir, "request-prompt-max-id")
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0o700), IsNil)
	f, err := os.Create(maxIDFilepath)
	c.Assert(err, IsNil)
	c.Assert(f.Chmod(0o400), IsNil)
	defer f.Chmod(0o600)

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, "cannot open request prompts backend:.*")
	c.Assert(mgr, IsNil)

	checkListenerClosed(c, reqChan)
}

func checkListenerClosed(c *C, reqChan <-chan *listener.Request) {
	select {
	case _, ok := <-reqChan:
		// reqChan was already closed
		c.Check(ok, Equals, false)
	case <-time.NewTimer(100 * time.Millisecond).C:
		c.Errorf("listener was not closed")
	}
}

func (s *apparmorpromptingSuite) TestNewErrorRuleDB(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Prevent rule backend from opening successfully
	maxIDFilepath := filepath.Join(prompting.StateDir(), "request-rule-max-id")
	c.Assert(os.MkdirAll(prompting.StateDir(), 0o700), IsNil)
	f, err := os.Create(maxIDFilepath)
	c.Assert(err, IsNil)
	c.Assert(f.Chmod(0o400), IsNil)
	defer f.Chmod(0o600)

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, "cannot open request rules backend:.*")
	c.Assert(mgr, IsNil)

	// Check that listener was closed
	checkListenerClosed(c, reqChan)
	// Ideally, we'd check that the prompt DB is also closed, but since the
	// InterfaceManager was never returned, it and the prompt DB pointed to
	// by it should be garbage collected, at least. The code calls Close(),
	// so we're pretty confident all is well.
}

func (s *apparmorpromptingSuite) TestStop(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	promptDB := mgr.PromptDB()
	c.Assert(promptDB, NotNil)
	ruleDB := mgr.RuleDB()
	c.Assert(ruleDB, NotNil)

	err = mgr.Stop()
	c.Check(err, IsNil)

	// Check that the listener and prompt and rule backends were closed
	checkListenerClosed(c, reqChan)
	c.Check(promptDB.Close(), Equals, requestprompts.ErrClosed)
	c.Check(ruleDB.Close(), Equals, requestrules.ErrClosed)

	// Check that current backends are nil
	c.Check(mgr.PromptDB(), IsNil)
	c.Check(mgr.RuleDB(), IsNil)
}

func (s *apparmorpromptingSuite) TestHandleListenerRequestDenyRoot(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Send request for root
	req := &listener.Request{
		// Most fields don't matter here
		SubjectUID: 0,
	}
	reqChan <- req
	// Should get immediate denial
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(resp.Request, Equals, req)
	c.Check(resp.Response.Allow, Equals, false)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestHandleListenerRequestErrors(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Send request with invalid permissions
	req := &listener.Request{
		// Most fields don't matter here
		SubjectUID: s.defaultUser,
		Permission: notify.FilePermission(0),
	}
	reqChan <- req
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(resp.Request, Equals, req)
	logger.WithLoggerLock(func() {
		c.Check(logbuf.String(), testutil.Contains,
			` error while parsing AppArmor permissions: cannot get abstract permissions from empty AppArmor permissions: "none"`)
	})

	// Fill the requestprompts backend until we hit its outstanding prompt
	// count limit
	maxOutstandingPromptsPerUser := 1000 // from requestprompts package
	for i := 0; i < maxOutstandingPromptsPerUser; i++ {
		req := &listener.Request{
			Label:      "snap.firefox.firefox",
			SubjectUID: s.defaultUser,
			Path:       fmt.Sprintf("/home/test/%d", i),
			Class:      notify.AA_CLASS_FILE,
			Permission: notify.AA_MAY_APPEND,
		}
		reqChan <- req
	}
	time.Sleep(10 * time.Millisecond)
	prompts, err = mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)
	c.Assert(len(prompts), Equals, maxOutstandingPromptsPerUser)

	// Now try to add one more request, it should fail
	logger.WithLoggerLock(func() {
		logbuf.Reset()
	})

	req = &listener.Request{
		Label:      "snap.firefox.firefox",
		SubjectUID: s.defaultUser,
		Path:       fmt.Sprintf("/home/test/%d", maxOutstandingPromptsPerUser),
		Class:      notify.AA_CLASS_FILE,
		Permission: notify.AA_MAY_APPEND,
	}
	reqChan <- req
	time.Sleep(10 * time.Millisecond)
	logger.WithLoggerLock(func() {
		c.Check(logbuf.String(), testutil.Contains,
			" WARNING: too many outstanding prompts for user 1000; auto-denying new one\n")
	})

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestHandleReplySimple(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	req, prompt := s.simulateRequest(c, reqChan, mgr, &listener.Request{}, false)

	// Reply to the request
	constraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	satisfied, err := mgr.HandleReply(s.defaultUser, prompt.ID, &constraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	// Simulate the listener receiving the response
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)

	c.Check(resp.Request, Equals, req)
	c.Check(resp.Response.Allow, Equals, true)
	aaPerms, err := prompting.AbstractPermissionsToAppArmorPermissions("home", constraints.Permissions)
	c.Check(err, IsNil)
	c.Check(resp.Response.Permission, Equals, aaPerms)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) simulateRequest(c *C, reqChan chan *listener.Request, mgr *apparmorprompting.InterfacesRequestsManager, req *listener.Request, shouldMerge bool) (*listener.Request, *requestprompts.Prompt) {
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	origPromptIDs := make(map[prompting.IDType]bool)
	for _, p := range prompts {
		origPromptIDs[p.ID] = true
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	// Simulate request from the kernel
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	// push a request
	reqChan <- req

	// Check that no error occurred
	time.Sleep(10 * time.Millisecond)
	logger.WithLoggerLock(func() { c.Assert(logbuf.String(), Equals, "") })

	// which should generate a notice
	s.st.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	n, err := s.st.WaitNotices(ctx, &state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	})
	s.st.Unlock()
	c.Check(err, IsNil)
	c.Check(n, HasLen, 1)

	// Check prompts now
	prompts, err = mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)

	if shouldMerge {
		c.Assert(prompts, HasLen, len(origPromptIDs))
		return req, nil
	}

	c.Assert(prompts, HasLen, len(origPromptIDs)+1)
	var prompt *requestprompts.Prompt
	for _, p := range prompts {
		if origPromptIDs[p.ID] {
			continue
		}
		prompt = p
		break
	}
	c.Assert(prompt, NotNil)
	expectedSnap := req.Label
	labelComponents := strings.Split(req.Label, ".")
	if len(labelComponents) == 3 {
		expectedSnap = labelComponents[1]
	}

	c.Check(prompt.Snap, Equals, expectedSnap)
	c.Check(prompt.Interface, Equals, "home")
	c.Check(prompt.Constraints.Path(), Equals, req.Path)

	// Check that we can query that prompt by ID
	promptByID, err := mgr.PromptWithID(s.defaultUser, prompt.ID)
	c.Check(err, IsNil)
	c.Check(promptByID, Equals, prompt)

	// Return manager, request, and prompt
	return req, prompt
}

// fillInPartialRequest fills in any blank fields from the given request
// with default non-empty values.
func (s *apparmorpromptingSuite) fillInPartialRequest(req *listener.Request) {
	if req.Label == "" {
		req.Label = "snap.firefox.firefox"
	}
	if req.SubjectUID == uint32(0) {
		req.SubjectUID = s.defaultUser
	}
	if req.Path == "" {
		req.Path = "/home/test/foo"
	}
	if req.Class == notify.MediationClass(0) {
		req.Class = notify.AA_CLASS_FILE
	}
	if req.Permission == nil {
		req.Permission = notify.AA_MAY_READ
	}
}

func mustParsePathPattern(c *C, pattern string) *patterns.PathPattern {
	parsed, err := patterns.ParsePathPattern(pattern)
	c.Assert(err, IsNil)
	return parsed
}

var errNoReply = errors.New("no reply received")

func waitForReply(replyChan chan apparmorprompting.RequestResponse) (*apparmorprompting.RequestResponse, error) {
	select {
	case resp := <-replyChan:
		return &resp, nil
	case <-time.NewTimer(100 * time.Millisecond).C:
		return nil, errNoReply
	}
}

func (s *apparmorpromptingSuite) TestHandleReplyErrors(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	_, prompt := s.simulateRequest(c, reqChan, mgr, &listener.Request{}, false)

	// Wrong user ID
	result, err := mgr.HandleReply(s.defaultUser+1, prompt.ID, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, Equals, requestprompts.ErrNotFound)
	c.Check(result, IsNil)

	// Wrong prompt ID
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID+1, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, Equals, requestprompts.ErrNotFound)
	c.Check(result, IsNil)

	// Invalid constraints
	invalidConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"foo"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &invalidConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, ErrorMatches, "invalid constraints.*")
	c.Check(result, IsNil)

	// Path not matched
	badPatternConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/other"),
		Permissions: []string{"read"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &badPatternConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, ErrorMatches, "constraints in reply do not match original request.*")
	c.Check(result, IsNil)

	// Permissions not matched
	badPermissionConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/foo"),
		Permissions: []string{"write"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &badPermissionConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, ErrorMatches, "replied permissions do not include all requested permissions.*")
	c.Check(result, IsNil)

	// Conflicting rule
	// For this, need to add another rule to the DB first, then try to reply
	// with a rule which conflicts with it. Reuse badPatternConstraints.
	newRule, err := mgr.AddRule(s.defaultUser, "firefox", "home", &badPatternConstraints, prompting.OutcomeAllow, prompting.LifespanTimespan, "10s")
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)
	conflictingConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/{foo,other}"),
		Permissions: []string{"read"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &conflictingConstraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Check(err, ErrorMatches, "cannot add rule.*")
	c.Check(result, IsNil)

	// Should not have received a reply
	_, err = waitForReply(replyChan)
	c.Assert(err, NotNil)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRuleAllowsNewPrompt(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Add allow rule to match read permission
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Add allow rule to match write permission
	constraints = &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"write"},
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Create request for read and write
	req := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	reqChan <- req
	time.Sleep(10 * time.Millisecond)

	// Check that no prompts were created
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Check that no notices were recorded
	s.st.Lock()
	n := s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	})
	s.st.Unlock()
	c.Check(n, HasLen, 0)

	// Check that kernel received a reply
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(resp.Request, Equals, req)
	c.Check(resp.Response.Allow, Equals, true)
	expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"read", "write"})
	c.Assert(err, IsNil)
	c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRulePartiallyAllowsNewPrompt(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Add rule to match read permission
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Do NOT add rule to match write permission

	// Create request for read and write
	partialReq := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	_, prompt := s.simulateRequest(c, reqChan, mgr, partialReq, false)

	// Check that prompt was created for remaining "write" permission
	c.Check(prompt.Constraints.RemainingPermissions(), DeepEquals, []string{"write"})

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRulePartiallyDeniesNewPrompt(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Add deny rule to match read permission
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeDeny, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Add no rule for write permissions

	// Create request for read and write
	req := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	reqChan <- req
	time.Sleep(10 * time.Millisecond)

	// Check that no prompts were created
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Check that no notices were recorded
	s.st.Lock()
	n := s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	})
	s.st.Unlock()
	c.Check(n, HasLen, 0)

	// Check that kernel received a reply
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(resp.Request, Equals, req)
	c.Check(resp.Response.Allow, Equals, true)
	c.Check(resp.Response.Permission, DeepEquals, notify.FilePermission(0))

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRulesMixedMatchNewPromptDenies(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Add deny rule to match read permission
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeDeny, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Add allow rule for write permissions
	constraints = &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"write"},
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Create request for read and write
	req := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	reqChan <- req
	time.Sleep(10 * time.Millisecond)

	// Check that no prompts were created
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Check that no notices were recorded
	s.st.Lock()
	n := s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	})
	s.st.Unlock()
	c.Check(n, HasLen, 0)

	// If there is an allow rule for some permissions and a deny rule for other
	// permissions, an allow response should be sent immediately for only the
	// previously-allowed permissions, and all denied permissions should be
	// automatically denied by the kernel.

	// Check that kernel received a reply
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(resp.Request, Equals, req)
	c.Check(resp.Response.Allow, Equals, true)
	expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"write"})
	c.Assert(err, IsNil)
	c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestNewRuleAllowExistingPrompt(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Add read request
	readReq := &listener.Request{
		Permission: notify.AA_MAY_READ,
	}
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Add request for write
	writeReq := &listener.Request{
		Permission: notify.AA_MAY_WRITE,
	}
	_, writePrompt := s.simulateRequest(c, reqChan, mgr, writeReq, false)

	// Add request for read and write
	rwReq := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	_, rwPrompt := s.simulateRequest(c, reqChan, mgr, rwReq, false)

	// Add rule to allow read request
	whenSent := time.Now()
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	rule, err := mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Check that kernel received a reply
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(resp.Request, Equals, readReq)
	c.Check(resp.Response.Allow, Equals, true)
	expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"read"})
	c.Assert(err, IsNil)
	c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)

	// Check that read request prompt was satisfied
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID)
	c.Check(err, NotNil)

	// Check that rwPrompt only has write permission left
	c.Check(rwPrompt.Constraints.RemainingPermissions(), DeepEquals, []string{"write"})

	// Check that two prompts still exist
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)
	c.Assert(prompts, HasLen, 2)
	if !(writePrompt == prompts[0] || writePrompt == prompts[1]) {
		c.Errorf("write prompt not found")
	}
	if !(rwPrompt == prompts[0] || rwPrompt == prompts[1]) {
		c.Errorf("rw prompt not found")
	}

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, DeepEquals, []*requestrules.Rule{rule})

	// Check that notices were recorded for read prompt and rw prompt,
	// and for the rule
	s.st.Lock()
	c.Check(s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	}), HasLen, 2)
	c.Check(s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsRuleUpdateNotice},
		After: whenSent,
	}), HasLen, 1)
	s.st.Unlock()

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestNewRuleDenyExistingPrompt(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Add read request
	readReq := &listener.Request{
		Permission: notify.AA_MAY_READ,
	}
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Add request for write
	writeReq := &listener.Request{
		Permission: notify.AA_MAY_WRITE,
	}
	_, writePrompt := s.simulateRequest(c, reqChan, mgr, writeReq, false)

	// Add request for read and write
	rwReq := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	_, rwPrompt := s.simulateRequest(c, reqChan, mgr, rwReq, false)

	// Add rule to deny read request
	whenSent := time.Now()
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	rule, err := mgr.AddRule(s.defaultUser, "firefox", "home", constraints, prompting.OutcomeDeny, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Check that kernel received two replies
	for i := 0; i < 2; i++ {
		resp, err := waitForReply(replyChan)
		c.Assert(err, IsNil)
		switch resp.Request {
		case readReq:
			c.Check(resp.Response.Allow, Equals, false)
			expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"read"})
			c.Assert(err, IsNil)
			c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)
		case rwReq:
			c.Check(resp.Response.Allow, Equals, false)
			expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"read", "write"})
			c.Assert(err, IsNil)
			c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)
		}
	}

	// Check that read and rw prompts were satisfied
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID)
	c.Check(err, NotNil)
	_, err = mgr.PromptWithID(s.defaultUser, rwPrompt.ID)
	c.Check(err, NotNil)

	// Check that one prompt still exists
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{writePrompt})

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, DeepEquals, []*requestrules.Rule{rule})

	// Check that notices were recorded for read prompt and rw prompt,
	// and for the rule
	s.st.Lock()
	c.Check(s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	}), HasLen, 2)
	c.Check(s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsRuleUpdateNotice},
		After: whenSent,
	}), HasLen, 1)
	s.st.Unlock()

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestReplyNewRuleHandlesExistingPrompt(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Already tested HandleReply errors, and that applyRuleToOutstandingPrompts
	// works correctly, so now just need to test that if reply creates a rule,
	// that rule applies to existing prompts.

	// Add read request
	readReq := &listener.Request{
		Permission: notify.AA_MAY_READ,
	}
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Add request for write
	writeReq := &listener.Request{
		Permission: notify.AA_MAY_WRITE,
	}
	_, writePrompt := s.simulateRequest(c, reqChan, mgr, writeReq, false)

	// Add request for read and write
	rwReq := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	_, rwPrompt := s.simulateRequest(c, reqChan, mgr, rwReq, false)

	// Reply to read prompt with denial
	whenSent := time.Now()
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	satisfiedPromptIDs, err := mgr.HandleReply(s.defaultUser, readPrompt.ID, constraints, prompting.OutcomeDeny, prompting.LifespanTimespan, "10s")
	c.Check(err, IsNil)

	// Check that rw prompt was also satisfied
	c.Check(satisfiedPromptIDs, DeepEquals, []prompting.IDType{rwPrompt.ID})

	// Check that kernel received two replies
	for i := 0; i < 2; i++ {
		resp, err := waitForReply(replyChan)
		c.Assert(err, IsNil)
		switch resp.Request {
		case readReq:
			c.Check(resp.Response.Allow, Equals, false)
			expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"read"})
			c.Assert(err, IsNil)
			c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)
		case rwReq:
			c.Check(resp.Response.Allow, Equals, false)
			expectedPermissions, err := prompting.AbstractPermissionsToAppArmorPermissions("home", []string{"read", "write"})
			c.Assert(err, IsNil)
			c.Check(resp.Response.Permission, DeepEquals, expectedPermissions)
		}
	}

	// Check that read and rw prompts no longer exist
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID)
	c.Check(err, NotNil)
	_, err = mgr.PromptWithID(s.defaultUser, rwPrompt.ID)
	c.Check(err, NotNil)

	// Check that one prompt still exists
	prompts, err := mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{writePrompt})

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, HasLen, 1)

	// Check that notices were recorded for read prompt and rw prompt,
	// and for the rule
	s.st.Lock()
	c.Check(s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	}), HasLen, 2)
	c.Check(s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsRuleUpdateNotice},
		After: whenSent,
	}), HasLen, 1)
	s.st.Unlock()

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestRequestMerged(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Requests with identical *original* abstract permissions are merged into
	// the existing prompt

	// Create request for read and write
	partialReq := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	_, prompt := s.simulateRequest(c, reqChan, mgr, partialReq, false)

	// Create identical request, it should merge
	identicalReq := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	s.simulateRequest(c, reqChan, mgr, identicalReq, true)

	// Add rule to satisfy the read permission
	constraints := &prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	_, err = mgr.AddRule(s.defaultUser, prompt.Snap, prompt.Interface, constraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Assert(err, IsNil)

	// Create identical request again, it should merge even though some
	// permissions have been satisfied
	identicalReqAgain := &listener.Request{
		Permission: notify.AA_MAY_READ | notify.AA_MAY_WRITE,
	}
	s.simulateRequest(c, reqChan, mgr, identicalReqAgain, true)

	// Now new requests for just write access will have identical remaining
	// permissions, but not identical original permissions, so should not merge
	readReq := &listener.Request{
		Permission: notify.AA_MAY_WRITE,
	}
	s.simulateRequest(c, reqChan, mgr, readReq, false)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestRules(c *C) {
	//reqChan, replyChan, restore := apparmorprompting.MockListener()
	//defer restore()

	//mgr, err := apparmorprompting.New(s.st)
	//c.Assert(err, IsNil)
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestRemoveRules(c *C) {
	//reqChan, replyChan, restore := apparmorprompting.MockListener()
	//defer restore()

	//mgr, err := apparmorprompting.New(s.st)
	//c.Assert(err, IsNil)
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestAddRuleWithIDPatchRemove(c *C) {
	//reqChan, replyChan, restore := apparmorprompting.MockListener()
	//defer restore()

	//mgr, err := apparmorprompting.New(s.st)
	//c.Assert(err, IsNil)
	c.Fatalf("TODO")
}
