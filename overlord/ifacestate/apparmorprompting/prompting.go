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

package apparmorprompting

import (
	"fmt"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

var (
	// Allow mocking the listener for tests
	listenerRegister = listener.Register
	listenerClose    = func(l *listener.Listener) error { return l.Close() }
	listenerRun      = func(l *listener.Listener) error { return l.Run() }
	listenerReqs     = func(l *listener.Listener) <-chan *listener.Request { return l.Reqs() }

	requestReply = func(req *listener.Request, allowedPermission any) error { return req.Reply(allowedPermission) }
)

// A Manager holds outstanding prompts and mediates their replies, further it
// stores and applies persistent rules.
type Manager interface {
	Prompts(userID uint32, clientActivity bool) ([]*requestprompts.Prompt, error)
	PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error)
	HandleReply(userID uint32, promptID prompting.IDType, replyConstraints *prompting.ReplyConstraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) ([]prompting.IDType, error)
	Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error)
	AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints) (*requestrules.Rule, error)
	RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error)
	RuleWithID(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error)
	PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatch *prompting.RuleConstraintsPatch) (*requestrules.Rule, error)
	RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error)
}

// verify that InterfacesRequestsManager implements Manager
var _ Manager = (*InterfacesRequestsManager)(nil)

type InterfacesRequestsManager struct {
	tomb tomb.Tomb
	// The lock should be held for writing when acting on the manager in a way
	// which requires synchronization between the prompts and rules databases,
	// or when removing those databases. The lock can be held for reading when
	// acting on just one or the other, as each has an internal mutex as well.
	lock     sync.RWMutex
	listener *listener.Listener
	prompts  *requestprompts.PromptDB
	rules    *requestrules.RuleDB

	notifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error
	notifyRule   func(userID uint32, ruleID prompting.IDType, data map[string]string) error
}

func New(s *state.State) (m *InterfacesRequestsManager, retErr error) {
	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		// TODO: add some sort of queue so that notifyPrompt calls can return
		// quickly without waiting for state lock and AddNotice() to return.
		s.Lock()
		defer s.Unlock()
		options := state.AddNoticeOptions{
			Data: data,
		}
		_, err := s.AddNotice(&userID, state.InterfacesRequestsPromptNotice, promptID.String(), &options)
		return err
	}
	notifyRule := func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		// TODO: add some sort of queue so that notifyRule calls can return
		// quickly without waiting for state lock and AddNotice() to return.
		s.Lock()
		defer s.Unlock()
		options := state.AddNoticeOptions{
			Data: data,
		}
		_, err := s.AddNotice(&userID, state.InterfacesRequestsRuleUpdateNotice, ruleID.String(), &options)
		return err
	}

	listenerBackend, err := listenerRegister()
	if err != nil {
		return nil, fmt.Errorf("cannot register prompting listener: %w", err)
	}
	defer func() {
		if retErr != nil {
			listenerClose(listenerBackend)
		}
	}()

	promptsBackend, err := requestprompts.New(notifyPrompt)
	if err != nil {
		return nil, fmt.Errorf("cannot open request prompts backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			promptsBackend.Close()
		}
	}()

	rulesBackend, err := requestrules.New(notifyRule)
	if err != nil {
		return nil, fmt.Errorf("cannot open request rules backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			rulesBackend.Close()
		}
	}()

	m = &InterfacesRequestsManager{
		listener:     listenerBackend,
		prompts:      promptsBackend,
		rules:        rulesBackend,
		notifyPrompt: notifyPrompt,
		notifyRule:   notifyRule,
	}

	m.tomb.Go(m.run)

	return m, nil
}

// Run is the main run loop for the manager, and must be called using tomb.Go.
func (m *InterfacesRequestsManager) run() error {
	m.lock.Lock()
	// disconnect replaces the listener so keep track of the one we have
	// right now
	currentListener := m.listener
	m.lock.Unlock()

	m.tomb.Go(func() error {
		logger.Debugf("starting prompting listener")
		if err := listenerRun(currentListener); err != listener.ErrClosed {
			return err
		}
		return nil
	})

run_loop:
	for {
		logger.Debugf("waiting prompt loop")
		select {
		case req, ok := <-listenerReqs(currentListener):
			if !ok {
				// Reqs() closed, so either errored or Stop() was called.
				// In either case, the listener Close() method has already
				// been called, and the tomb error will be set to the return
				// value of the Run() call from the previous tracked goroutine.
				logger.Debugf("prompting listener closed requests channel")
				break run_loop
			}

			logger.Debugf("received from kernel requests channel: %v", req)
			if err := m.handleListenerReq(req); err != nil {
				logger.Noticef("error while handling request: %+v", err)
			}
		case <-m.tomb.Dying():
			logger.Noticef("InterfacesRequestsManager tomb is dying, disconnecting")
			break run_loop
		}
	}
	return m.disconnect()
}

func (m *InterfacesRequestsManager) handleListenerReq(req *listener.Request) error {
	userID := uint32(req.SubjectUID)
	if userID == 0 {
		// Deny any request for the root user
		return requestReply(req, nil)
	}
	snap := req.Label // Default to apparmor label, in case process is not a snap
	tag, err := naming.ParseSecurityTag(req.Label)
	if err == nil {
		// the triggering process is not a snap, so treat apparmor label as snap field
		snap = tag.InstanceName()
	}

	// TODO: when we support interfaces beyond "home", do a proper selection here
	iface := "home"

	path := req.Path

	permissions, err := prompting.AbstractPermissionsFromAppArmorPermissions(iface, req.Permission)
	if err != nil {
		logger.Noticef("error while parsing AppArmor permissions: %v", err)
		return requestReply(req, nil)
	}

	// we're done with early checks, serious business starts now, and we can
	// take the lock
	m.lock.Lock()
	defer m.lock.Unlock()

	allowedPerms, matchedDenyRule, outstandingPerms, err := m.rules.IsRequestAllowed(userID, snap, iface, path, permissions)
	if err != nil || matchedDenyRule || len(outstandingPerms) == 0 {
		switch {
		case err != nil:
			logger.Noticef("error while checking request against existing rules: %v", err)
		case matchedDenyRule:
			logger.Debugf("request denied by existing rule: %+v", req)
		case len(outstandingPerms) == 0:
			logger.Debugf("request allowed by existing rule: %+v", req)
		}
		// Allow any requested permissions which were explicitly allowed by
		// existing rules (there may be no such permissions) and let the
		// listener deny all permissions which were not explicitly included in
		// the allowed permissions.
		allowedPermission, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, allowedPerms)
		// Error should not occur, but if it does, allowedPermission is set to
		// empty, leaving it to the listener to default deny all permissions.
		return requestReply(req, allowedPermission)
	}

	// Request not satisfied by any of existing rules, record a prompt for the user

	metadata := &prompting.Metadata{
		User:      userID,
		Snap:      snap,
		Interface: iface,
	}

	newPrompt, merged, err := m.prompts.AddOrMerge(metadata, path, permissions, outstandingPerms, req)
	if err != nil {
		logger.Noticef("error while checking request against prompt DB: %v", err)

		// We weren't able to create a new prompt, so respond with the best
		// information we have, which is to allow any permissions which were
		// allowed by existing rules, and let the listener deny the rest.
		allowedPermission, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, allowedPerms)
		// Error should not occur, but if it does, allowedPermission is set to
		// empty, leaving it to the listener to default deny all permissions.
		return requestReply(req, allowedPermission)
	}

	if merged {
		logger.Debugf("new prompt merged with identical existing prompt: %+v", newPrompt)
	} else {
		logger.Debugf("adding prompt to internal storage: %+v", newPrompt)
	}

	return nil
}

func (m *InterfacesRequestsManager) disconnect() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	var errs []error
	if m.listener != nil {
		errs = append(errs, listenerClose(m.listener))
		m.listener = nil
	}
	if m.prompts != nil {
		errs = append(errs, m.prompts.Close())
		m.prompts = nil
	}
	if m.rules != nil {
		errs = append(errs, m.rules.Close())
		m.rules = nil
	}

	return strutil.JoinErrors(errs...)
}

// Stop closes the listener, prompt DB, and rule DB. Stop is idempotent, and
// the receiver cannot be started or used after it has been stopped.
func (m *InterfacesRequestsManager) Stop() error {
	m.tomb.Kill(nil)
	// Kill causes the run loop to exit and call disconnect()
	return m.tomb.Wait()
}

// Prompts returns all prompts for the user with the given user ID.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) Prompts(userID uint32, clientActivity bool) ([]*requestprompts.Prompt, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.prompts.Prompts(userID, clientActivity)
}

// PromptWithID returns the prompt with the given ID for the given user.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.prompts.PromptWithID(userID, promptID, clientActivity)
}

// HandleReply checks that the given reply contents are valid, satisfies the
// original request, and does not conflict with any existing rules (if lifespan
// is not "single"). If all of these are true, sends a reply for the prompt with
// the given ID, and both creates a new rule and checks any outstanding prompts
// against it, if the lifespan is not "single".
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) HandleReply(userID uint32, promptID prompting.IDType, replyConstraints *prompting.ReplyConstraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) (satisfiedPromptIDs []prompting.IDType, retErr error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	prompt, err := m.prompts.PromptWithID(userID, promptID, clientActivity)
	if err != nil {
		return nil, err
	}

	// Validate reply constraints and convert them to Constraints, which have
	// dedicated PermissionEntry values for each permission in the reply.
	// Outcome and lifespan are validated while unmarshalling, and duration is
	// validated against the given lifespan when constructing the Constraints.
	constraints, err := replyConstraints.ToConstraints(prompt.Interface, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}

	// Check that constraints matches original requested path.
	// AppArmor is responsible for pre-vetting that all paths which appear
	// in requests from the kernel are allowed by the appropriate
	// interfaces, so we do not assert anything else particular about the
	// constraints, such as check that the path pattern does not match
	// any paths not granted by the interface.
	// TODO: Should this be reconsidered?
	matches, err := constraints.Match(prompt.Constraints.Path())
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, &prompting_errors.RequestedPathNotMatchedError{
			Requested: prompt.Constraints.Path(),
			Replied:   constraints.PathPattern.String(),
		}
	}

	// XXX: do we want to allow only replying to a select subset of permissions, and
	// auto-deny the rest?
	contained := constraints.ContainPermissions(prompt.Constraints.OutstandingPermissions())
	if !contained {
		return nil, &prompting_errors.RequestedPermissionsNotMatchedError{
			Requested: prompt.Constraints.OutstandingPermissions(),
			Replied:   replyConstraints.Permissions, // equivalent to keys of constraints.Permissions
		}
	}

	// It is important that a lock is held while checking for conflicts with
	// other rules so that if the rule is eventually removed due to an error,
	// no prompts can have been matched against it in the meantime.
	var newRule *requestrules.Rule
	if lifespan != prompting.LifespanSingle {
		// Check that adding the rule doesn't conflict with other rules
		newRule, err = m.rules.AddRule(userID, prompt.Snap, prompt.Interface, constraints)
		if err != nil {
			// Rule conflicts with existing rule (at least one identical pattern
			// variant and permission). This should be considered a bad reply,
			// since the user should only be prompted for permissions and paths
			// which are not already covered.
			return nil, err
		}

		defer func() {
			if retErr != nil || lifespan == prompting.LifespanSingle {
				m.rules.RemoveRule(userID, newRule.ID)
			}
		}()
	}

	prompt, retErr = m.prompts.Reply(userID, promptID, outcome, clientActivity)
	if retErr != nil {
		// Error should not occur unless the listener has closed
		return nil, retErr
	}

	if lifespan == prompting.LifespanSingle {
		return []prompting.IDType{}, nil
	}

	// Apply new rule to outstanding prompts.
	satisfiedPromptIDs = m.applyRuleToOutstandingPrompts(newRule)

	return satisfiedPromptIDs, nil
}

func (m *InterfacesRequestsManager) applyRuleToOutstandingPrompts(rule *requestrules.Rule) []prompting.IDType {
	metadata := &prompting.Metadata{
		User:      rule.User,
		Snap:      rule.Snap,
		Interface: rule.Interface,
	}
	satisfiedPromptIDs, err := m.prompts.HandleNewRule(metadata, rule.Constraints)
	if err != nil {
		// The rule's constraints and outcome were already validated, so an
		// error should not occur here unless the prompt DB was already closed.
		logger.Noticef("error when handling new rule: %v", err)
	}
	return satisfiedPromptIDs
}

// Rules returns all rules for the user with the given user ID and,
// optionally, only those for the given snap and/or interface.
func (m *InterfacesRequestsManager) Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if snap != "" {
		if iface != "" {
			rules := m.rules.RulesForSnapInterface(userID, snap, iface)
			return rules, nil
		}
		rules := m.rules.RulesForSnap(userID, snap)
		return rules, nil
	}
	if iface != "" {
		rules := m.rules.RulesForInterface(userID, iface)
		return rules, nil
	}
	rules := m.rules.Rules(userID)
	return rules, nil
}

// AddRule creates a new rule with the given contents and then checks it against
// outstanding prompts, resolving any prompts which it satisfies.
func (m *InterfacesRequestsManager) AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints) (*requestrules.Rule, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	newRule, err := m.rules.AddRule(userID, snap, iface, constraints)
	if err != nil {
		return nil, err
	}
	// Apply new rule to outstanding prompts.
	m.applyRuleToOutstandingPrompts(newRule)
	return newRule, nil
}

// RemoveRules removes all rules for the user with the given user ID and the
// given snap and/or interface. Snap and iface can't both be unspecified.
func (m *InterfacesRequestsManager) RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	// The lock need only be held for reading, since no synchronization is
	// required between the rules and prompts backends, and the rules backend
	// has an internal mutex.
	m.lock.RLock()
	defer m.lock.RUnlock()

	if snap == "" && iface == "" {
		// The caller should ensure that this is not the case.
		return nil, fmt.Errorf("cannot remove rules for unspecified snap and interface")
	}
	if snap != "" {
		if iface != "" {
			return m.rules.RemoveRulesForSnapInterface(userID, snap, iface)
		} else {
			return m.rules.RemoveRulesForSnap(userID, snap)
		}
	}
	return m.rules.RemoveRulesForInterface(userID, iface)
}

// RuleWithID returns the rule with the given ID for the given user.
func (m *InterfacesRequestsManager) RuleWithID(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	rule, err := m.rules.RuleWithID(userID, ruleID)
	return rule, err
}

// PatchRule updates the rule with the given ID using the provided contents.
// Any of the given fields which are empty/nil are not updated in the rule.
func (m *InterfacesRequestsManager) PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatch *prompting.RuleConstraintsPatch) (*requestrules.Rule, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	patchedRule, err := m.rules.PatchRule(userID, ruleID, constraintsPatch)
	if err != nil {
		return nil, err
	}
	// Apply patched rule to outstanding prompts.
	m.applyRuleToOutstandingPrompts(patchedRule)
	return patchedRule, nil
}

func (m *InterfacesRequestsManager) RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	// The lock need only be held for reading, since no synchronization is
	// required between the rules and prompts backends, and the rules backend
	// has an internal mutex.
	m.lock.RLock()
	defer m.lock.RUnlock()

	rule, err := m.rules.RemoveRule(userID, ruleID)
	return rule, err
}
