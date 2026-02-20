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
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

var (
	// Allow mocking the listener for tests
	listenerRegister = func() (listenerBackend, error) {
		return listener.Register(prompting.NewRequestFromListener)
	}

	cgroupProcessPathInTrackingCgroup = cgroup.ProcessPathInTrackingCgroup
)

type listenerBackend interface {
	Close() error
	Run() error
	Ready() <-chan struct{}
	Reqs() <-chan *prompting.Request
}

// A Manager holds outstanding prompts and mediates their replies, further it
// stores and applies persistent rules.
type Manager interface {
	Prompts(userID uint32, clientActivity bool) ([]*requestprompts.Prompt, error)
	PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error)
	HandleReply(userID uint32, promptID prompting.IDType, replyConstraintsJSON prompting.ConstraintsJSON, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) ([]prompting.IDType, error)
	Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error)
	AddRule(userID uint32, snap string, iface string, constraintsJSON prompting.ConstraintsJSON) (*requestrules.Rule, error)
	RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error)
	RuleWithID(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error)
	PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatchJSON prompting.ConstraintsJSON) (*requestrules.Rule, error)
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
	listener listenerBackend
	prompts  *requestprompts.PromptDB
	rules    *requestrules.RuleDB

	// listenerAlreadySignalled is closed when the listener readiness is first
	// observed. If there are still pending unreceived requests from outside
	// the listener, then listener readiness will not cause the prompts backend
	// to ready. Thus, this channel is used to avoid repeatedly observing the
	// listener readiness.
	listenerAlreadySignalled chan struct{}

	apiRequests chan *prompting.Request

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
			listenerBackend.Close()
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
		listener:                 listenerBackend,
		prompts:                  promptsBackend,
		rules:                    rulesBackend,
		listenerAlreadySignalled: make(chan struct{}),
		apiRequests:              make(chan *prompting.Request),
		notifyPrompt:             notifyPrompt,
		notifyRule:               notifyRule,
	}

	m.tomb.Go(m.run)

	return m, nil
}

// Run is the main run loop for the manager, and must be called using tomb.Go.
func (m *InterfacesRequestsManager) run() error {
	m.tomb.Go(func() error {
		logger.Debugf("starting prompting listener")
		// listener.Run will return an error if and only if there's a real
		// error, not if Close() is called. But Close() is called only by
		// disconnect(), which is itself only called when this run() function
		// returns, which only occurs when the manager tomb is dying. So we
		// don't need to worry about the listener returning nil when we don't
		// already expect to be exiting.
		return m.listener.Run()
	})

run_loop:
	for {
		logger.Debugf("waiting prompt loop")
		select {
		case <-m.listenerReadyForTheFirstTime():
			logger.Debugf("received ready signal from the listener")
			close(m.listenerAlreadySignalled)
			// Tell the requestprompts backend that the listener is ready, so
			// it can discard ID mappings for requests which have not been
			// re-received from the kernel. For completeness, acquire the lock,
			// though this shouldn't really be necessary since the API
			// endpoints are still waiting on the requestprompts backend to
			// signal readiness. The lock only needs to be held for reading,
			// since no synchronization is required between the rules and
			// prompts backends, and the prompts backend has an internal mutex.
			m.lock.RLock()
			prunedRequestKeys := m.prompts.HandleReadying("kernel")
			if len(prunedRequestKeys) > 0 {
				logger.Noticef("requests timed out in the kernel while snapd was restarting: %s", strutil.Quoted(prunedRequestKeys))
			} else {
				logger.Debugf("listener signalled readiness and no outstanding requests were pruned")
			}
			// If there are no pending un-received requests from outside the
			// kernel, then this will unblock API method calls.
			m.lock.RUnlock()
		case req, ok := <-m.listener.Reqs():
			if !ok {
				// Reqs() closed, so an error occurred in the listener. In
				// production, the listener does not close itself on error, so
				// this should never actually occur.
				//
				// If Stop() were called, it would set the tomb to dying and
				// the other select case would have occurred, since the
				// disconnect() method is the only place in the manager where
				// the listener Close() method is called, and disconnect() is
				// only called when this function returns.
				//
				// The listener Close() method has already been called by the
				// listener itself, and the tomb error will be set to the error
				// value of the Run() call from the previous tracked goroutine.
				logger.Debugf("prompting listener closed requests channel")
				break run_loop
			}

			logger.Debugf("received from kernel requests channel: %+v", req)
			if err := m.handleRequest(req); err != nil {
				logger.Noticef("error while handling request: %+v", err)
			}
		case req := <-m.apiRequests:
			logger.Debugf("received from API request channel: %+v", req)
			if err := m.handleRequest(req); err != nil {
				logger.Noticef("error while handling request: %+v", err)
			}
		case <-m.tomb.Dying():
			logger.Debugf("InterfacesRequestsManager tomb is dying with error %v, disconnecting", m.tomb.Err())
			break run_loop
		}
	}
	return m.disconnect()
}

func (m *InterfacesRequestsManager) listenerReadyForTheFirstTime() <-chan struct{} {
	select {
	case <-m.prompts.Ready():
		// The prompts backend already readied, so we can ignore the listener
		// signalling readiness. Most likely, the listener previously readied
		// and that caused the prompts backend to ready. Alternatively, the
		// prompts backend timed out waiting for requests to be re-received.
		// Either way, the listener readying doesn't matter anymore, so return
		// something which will never signal.
		return nil
	case <-m.listenerAlreadySignalled:
		// Listener already signalled ready and we already observed it.
		return nil
	default:
		// We haven't handled a ready signal yet, so return the real thing.
		return m.listener.Ready()
	}
}

func (m *InterfacesRequestsManager) handleRequest(req *prompting.Request) error {
	if req.UID == 0 {
		// Deny any request for the root user
		return req.Reply(nil)
	}
	snap := req.AppArmorLabel // Default to apparmor label, in case process is not a snap
	if tag, err := naming.ParseSecurityTag(req.AppArmorLabel); err == nil {
		// the triggering process is a snap, so use instance name as snap field
		snap = tag.InstanceName()
	}

	// we're done with early checks, serious business starts now, and we can
	// take the lock
	m.lock.Lock()
	defer m.lock.Unlock()

	allowedPerms, matchedDenyRule, outstandingPerms, err := m.rules.IsRequestAllowed(req.UID, snap, req.Interface, req.Path, req.Permissions)
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
		// existing rules (there may be no such permissions) and auto-deny all
		// permissions which were not explicitly included in the allowed permissions.
		return req.Reply(allowedPerms)
	}

	// Request not satisfied by any of existing rules, record a prompt for the user

	metadata := &prompting.Metadata{
		User:      req.UID,
		Snap:      snap,
		PID:       req.PID,
		Cgroup:    req.Cgroup,
		Interface: req.Interface,
	}
	// TODO: metadata isn't really necessary, since req holds almost all info;
	// or, req isn't really necessary, and could instead just pass Reply() ?

	newPrompt, merged, err := m.prompts.AddOrMerge(metadata, req.Path, req.Permissions, outstandingPerms, req)
	if err != nil {
		logger.Noticef("error while checking request against prompt DB: %v", err)

		// We weren't able to create a new prompt, so respond with the best
		// information we have, which is to allow any permissions which were
		// allowed by existing rules, and auto-deny the rest.
		return req.Reply(allowedPerms)
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
		errs = append(errs, m.listener.Close())
	}
	if m.rules != nil {
		errs = append(errs, m.rules.Close())
	}
	// Closing m.prompts will unblock API requests, if they are still blocked.
	if m.prompts != nil {
		errs = append(errs, m.prompts.Close())
	}

	return strutil.JoinErrors(errs...)
}

// Query creates a request with the given contents and feeds it into the
// prompting manager, either matching it against an existing rule or creating
// a prompt and waiting for a reply.
func (m *InterfacesRequestsManager) Query(uid uint32, pid int32, apparmorLabel string, iface string) (prompting.OutcomeType, error) {
	cgroup, err := cgroupProcessPathInTrackingCgroup(int(pid))
	if err != nil {
		return prompting.OutcomeUnset, fmt.Errorf("cannot read cgroup path for request process with PID %d: %w", pid, err)
	}
	permissions, err := prompting.AvailablePermissions(iface)
	if err != nil {
		return prompting.OutcomeUnset, err
	}
	key := fmt.Sprintf("api:%s:%d:%d:%s", iface, uid, pid, apparmorLabel)
	// We need a placeholder path until we can work with requests/prompts/rules
	// for interfaces which don't care about paths. This placeholder path will
	// not be included in prompts, and path patterns for rules for interfaces
	// with requests from the API will always match it.
	// TODO: once paths are not necessary for all interfaces, remove this.
	const path = "/api-request-placeholder"

	replyChan := make(chan []string)

	req := &prompting.Request{
		Key:           key,
		UID:           uid,
		PID:           pid,
		Cgroup:        cgroup,
		AppArmorLabel: apparmorLabel,
		Interface:     iface,
		Permissions:   permissions,
		Path:          path,
		Reply: func(allowedPerms []string) error {
			select {
			case replyChan <- allowedPerms:
				return nil
			case <-m.tomb.Dying():
				return prompting_errors.ErrPromptingClosed
			}
		},
	}

	// Send request to manager
	select {
	case m.apiRequests <- req:
		// request received and being processed
	case <-m.tomb.Dying():
		return prompting.OutcomeUnset, prompting_errors.ErrPromptingClosed
	}

	// Wait for reply
	var allowedPermissions []string
	select {
	case allowedPermissions = <-replyChan:
		// received reply
	case <-m.tomb.Dying():
		return prompting.OutcomeUnset, prompting_errors.ErrPromptingClosed
	}

	// If any requested permissions are not allowed, then reply with OutcomeDeny
	for _, perm := range permissions {
		if !strutil.ListContains(allowedPermissions, perm) {
			return prompting.OutcomeDeny, nil
		}
	}
	return prompting.OutcomeAllow, nil
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
	<-m.prompts.Ready()

	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.prompts.Prompts(userID, clientActivity)
}

// PromptWithID returns the prompt with the given ID for the given user.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error) {
	<-m.prompts.Ready()

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
func (m *InterfacesRequestsManager) HandleReply(userID uint32, promptID prompting.IDType, replyConstraintsJSON prompting.ConstraintsJSON, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) (satisfiedPromptIDs []prompting.IDType, retErr error) {
	<-m.prompts.Ready()

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
	constraints, err := prompting.UnmarshalReplyConstraints(prompt.Interface, outcome, lifespan, duration, replyConstraintsJSON)
	if err != nil {
		return nil, fmt.Errorf("cannot decode request body into prompt reply: %w", err)
	}

	// Check that constraints matches original requested path.
	// We do not assert anything else particular about the constraints, such
	// as check that the path pattern does not match any paths not granted by
	// the interface.
	// TODO: Should this be reconsidered?
	matches, err := constraints.PathPattern().Match(prompt.Constraints.Path())
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, &prompting_errors.RequestedPathNotMatchedError{
			Requested: prompt.Constraints.Path(),
			Replied:   constraints.PathPattern().String(),
		}
	}

	// XXX: do we want to allow only replying to a select subset of permissions, and
	// auto-deny the rest?
	contained := constraints.ContainPermissions(prompt.Constraints.OutstandingPermissions())
	if !contained {
		// We never expose the original list of permissions in the reply,
		// so we need to reconstruct it from the keys in the permission map.
		// Thus, the permissions will no longer be in their original order.
		replyPermissions := make([]string, 0, len(constraints.Permissions))
		for perm := range constraints.Permissions {
			replyPermissions = append(replyPermissions, perm)
		}
		return nil, &prompting_errors.RequestedPermissionsNotMatchedError{
			Requested: prompt.Constraints.OutstandingPermissions(),
			Replied:   replyPermissions,
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
			if retErr != nil {
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
func (m *InterfacesRequestsManager) AddRule(userID uint32, snap string, iface string, constraintsJSON prompting.ConstraintsJSON) (*requestrules.Rule, error) {
	<-m.prompts.Ready()

	m.lock.Lock()
	defer m.lock.Unlock()

	constraints, err := prompting.UnmarshalConstraints(iface, constraintsJSON)
	if err != nil {
		return nil, fmt.Errorf("cannot decode request body for rules endpoint: %w", err)
	}

	newRule, err := m.rules.AddRule(userID, snap, iface, constraints)
	if err != nil {
		return nil, err
	}
	m.applyRuleToOutstandingPrompts(newRule)
	return newRule, nil
}

// RemoveRules removes all rules for the user with the given user ID and the
// given snap and/or interface. Snap and iface can't both be unspecified.
func (m *InterfacesRequestsManager) RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	<-m.prompts.Ready()

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
func (m *InterfacesRequestsManager) PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatchJSON prompting.ConstraintsJSON) (*requestrules.Rule, error) {
	<-m.prompts.Ready()

	m.lock.Lock()
	defer m.lock.Unlock()

	// Lookup existing rule only so we can get its interface, so we can
	// unmarshal the constraints patch from json.
	origRule, err := m.rules.RuleWithID(userID, ruleID)
	if err != nil {
		return nil, err
	}
	constraintsPatch, err := prompting.UnmarshalRuleConstraintsPatch(origRule.Interface, constraintsPatchJSON)
	if err != nil {
		// XXX: should this say "... or deletion" like daemon does?
		return nil, fmt.Errorf("cannot decode request body into request rule modification: %w", err)
	}

	patchedRule, err := m.rules.PatchRule(userID, ruleID, constraintsPatch)
	if err != nil {
		return nil, err
	}
	// Apply patched rule to outstanding prompts.
	m.applyRuleToOutstandingPrompts(patchedRule)
	return patchedRule, nil
}

func (m *InterfacesRequestsManager) RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	<-m.prompts.Ready()

	// The lock need only be held for reading, since no synchronization is
	// required between the rules and prompts backends, and the rules backend
	// has an internal mutex.
	m.lock.RLock()
	defer m.lock.RUnlock()

	rule, err := m.rules.RemoveRule(userID, ruleID)
	return rule, err
}
