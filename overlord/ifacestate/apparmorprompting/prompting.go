package apparmorprompting

import (
	"errors"
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/requestprompts"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/requestrules"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
)

var (
	ErrPromptingNotEnabled = errors.New("AppArmor Prompting is not enabled")
)

// TODO: replace with the following in ifacemgr.go:
//func (m *InterfaceManager) AppArmorPromptingRunning() bool {
//	return m.useAppArmorPrompting()
//}
var PromptingEnabled = func() bool {
	return features.AppArmorPrompting.IsEnabled() && notify.SupportAvailable()
}

type Interface interface {
	Connect() error
	Run() error
	Stop() error
}

type Prompting struct {
	tomb     tomb.Tomb
	listener *listener.Listener
	prompts  *requestprompts.PromptDB
	rules    *requestrules.RuleDB

	notifyPrompt func(userID uint32, promptID string, options *state.AddNoticeOptions) error
	notifyRule   func(userID uint32, ruleID string, options *state.AddNoticeOptions) error
}

func New(s *state.State) Interface {
	notifyPrompt := func(userID uint32, promptID string, options *state.AddNoticeOptions) error {
		s.Lock()
		defer s.Unlock()
		_, err := s.AddNotice(&userID, state.RequestsPromptNotice, promptID, options)
		return err
	}
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		s.Lock()
		defer s.Unlock()
		_, err := s.AddNotice(&userID, state.RequestsRuleUpdateNotice, ruleID, options)
		return err
	}
	p := &Prompting{
		notifyPrompt: notifyPrompt,
		notifyRule:   notifyRule,
	}
	return p
}

func (p *Prompting) Connect() error {
	if !PromptingEnabled() {
		return nil
	}
	if p.prompts != nil {
		return fmt.Errorf("cannot connect: listener is already registered")
	}
	l, err := listenerRegister()
	if err != nil {
		return fmt.Errorf("cannot register prompting listener: %v", err)
	}
	p.listener = l
	p.prompts = requestprompts.New(p.notifyPrompt)
	p.rules, _ = requestrules.New(p.notifyRule) // ignore error (failed to load existing rules)
	return nil
}

var (
	notifySupportAvailable = notify.SupportAvailable
	listenerRegister       = listener.Register
)

func (p *Prompting) disconnect() error {
	if p.listener == nil {
		return nil
	}
	defer func() {
		p.listener = nil
	}()
	if err := listenerClose(p.listener); err != nil {
		return err
	}
	return nil
}

var listenerClose = func(l *listener.Listener) error {
	return l.Close()
}

func (p *Prompting) Run() error {
	if !PromptingEnabled() {
		return nil
	}
	p.tomb.Go(func() error {
		if p.listener == nil {
			logger.Noticef("listener is nil, exiting Prompting.Run() early")
			return fmt.Errorf("listener is nil, cannot run AppArmor prompting")
		}
		p.tomb.Go(func() error {
			logger.Noticef("starting listener")
			if err := listenerRun(p.listener); err != listener.ErrClosed {
				return err
			}
			return nil
		})

		logger.Noticef("ready for prompts")
		for {
			logger.Debugf("waiting prompt loop")
			select {
			case req, ok := <-listenerReqs(p.listener):
				if !ok {
					// Reqs() closed, so either errored or Stop() was called.
					// In either case, the listener Close() method has already
					// been called, and the tomb error will be set to the return
					// value of the Run() call from the previous tracked goroutine.
					logger.Noticef("listener closed requests channel")
					return p.disconnect()
				}
				logger.Noticef("Got from kernel req chan: %v", req)
				if err := p.handleListenerReq(req); err != nil { // no use multithreading, since IsPathAllowed locks
					logger.Noticef("Error while handling request: %+v", err)
				}
			case <-p.tomb.Dying():
				logger.Noticef("Prompting tomb is dying, disconnecting")
				return p.disconnect()
			}
		}
	})
	return nil // TODO: finish this function (is it finished??)
}

var (
	listenerRun = func(l *listener.Listener) error {
		return l.Run()
	}
	listenerReqs = func(l *listener.Listener) <-chan *listener.Request {
		return l.Reqs()
	}
)

func (p *Prompting) handleListenerReq(req *listener.Request) error {
	userID := uint32(req.SubjectUID())
	snap, err := common.LabelToSnap(req.Label())
	if err != nil {
		// the triggering process is not a snap, so treat apparmor label as snap field
	}

	iface := common.SelectSingleInterface(req.Interfaces())

	path := req.Path()

	permissions, err := common.AbstractPermissionsFromAppArmorPermissions(iface, req.Permission())
	if err != nil {
		logger.Noticef("error while parsing AppArmor permissions: %v", err)
		// XXX: is it better to auto-deny here or auto-allow?
		return req.Reply(false)
	}

	remainingPerms := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		if yesNo, err := p.rules.IsPathAllowed(userID, snap, iface, path, perm); err == nil {
			if !yesNo {
				logger.Noticef("request denied by existing rule: %+v", req)
				// TODO: the response puts all original permissions in the
				// Deny field, do we want to differentiate the denied bits from
				// the others?
				return req.Reply(false)
			}
		} else {
			// No matching rule found
			remainingPerms = append(remainingPerms, perm)
		}
	}
	if len(remainingPerms) == 0 {
		logger.Noticef("request allowed by existing rule: %+v", req)
		return req.Reply(true)
	}

	newPrompt, merged := p.prompts.AddOrMerge(userID, snap, iface, path, remainingPerms, req)
	if merged {
		logger.Noticef("new prompt merged with identical existing prompt: %+v", newPrompt)
		return nil
	}

	logger.Noticef("adding prompt to internal storage: %+v", newPrompt)

	return nil
}

func (p *Prompting) Stop() error {
	p.tomb.Kill(nil)
	return p.tomb.Wait()
}

// GetPrompts returns all prompts for the user with the given user ID.
func (p *Prompting) GetPrompts(userID uint32) ([]*requestprompts.Prompt, error) {
	// TODO: when we switch from o/i/a/requestprompts to i/p/requestprompts,
	// return error from Prompts() instead of nil
	return p.prompts.Prompts(userID), nil
}

// GetPromptWithID returns the prompt with the given ID for the given user.
func (p *Prompting) GetPromptWithID(userID uint32, promptID string) (*requestprompts.Prompt, error) {
	return p.prompts.PromptWithID(userID, promptID)
}

// HandleReply checks that the given reply contents are valid, satisfies the
// original request, and does not conflict with any existing rules (if lifespan
// is not "single"). If all of these are true, sends a reply for the prompt with
// the given ID, and both creates a new rule and checks any outstanding prompts
// against it, if the lifespan is not "single".
func (p *Prompting) HandleReply(userID uint32, promptID string, constraints *common.Constraints, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (satisfiedPromptIDs []string, retErr error) {
	prompt, err := p.prompts.PromptWithID(userID, promptID)
	if err != nil {
		return nil, err
	}

	// TODO: when we switch from o/i/a/common to i/prompting, no need to
	// validate outcome and lifespan, as OutcomeType and LifespanType are both
	// validated while unmarshalling, and duration is validated when the rule
	// is being added. Path pattern (within constraints) is also validated, so
	// the only manual validation required is permissions, which can be done
	// via constraints.ValidateForInterface()
	if _, err := common.ValidateConstraintsOutcomeLifespanDuration(prompt.Interface, constraints, outcome, lifespan, duration); err != nil {
		return nil, err
	}

	// Check that constraints matches original requested path.
	// AppArmor is responsible for pre-vetting that all paths which appear
	// in requests from the kernel are allowed by the appropriate
	// interfaces, so we do not assert anything else particular about the
	// constraints, such as check that the path pattern does not match
	// any paths not granted by the interface.
	// TODO: Should this be reconsidered?
	matches, err := constraints.Match(prompt.Constraints.Path)
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, fmt.Errorf("constraints in reply do not match original request: '%v' does not match '%v'; please try again", constraints, prompt.Constraints)
	}

	// TODO: once support for sending back bitmask of allowed permissions lands,
	// do we want to allow only replying to a select subset of permissions, and
	// auto-deny the rest?
	contained := constraints.ContainPermissions(prompt.Constraints.Permissions)
	if !contained {
		return nil, fmt.Errorf("replied permissions do not include all requested permissions: requested %v, replied %v; please try again", prompt.Constraints.Permissions, constraints.Permissions)
	}

	// TODO: a lock should be held while checking for conflicts with other rules
	// so that if the rule is eventually removed due to an error, no prompts can
	// have been matched against it in the meantime.
	// A RWMutex over prompts and rules should work well, and could potentially
	// replace the internal mutexes in those packages.
	var newRule *requestrules.Rule
	if lifespan != common.LifespanSingle {
		// Check that adding the rule doesn't conflict with other rules
		newRule, err = p.rules.AddRule(userID, prompt.Snap, prompt.Interface, constraints, outcome, lifespan, duration)
		if err != nil {
			// Rule conflicts with existing rule (at least one identical pattern
			// variant and permission). This should be considered a bad reply,
			// since the user should only be prompted for permissions and paths
			// which are not already covered.

			// TODO: there are scenarios where this could reasonably happen, so
			// better to retry adding the new rule after removing any conflicts
			// with existing rules. Likely, the new rule should replace the old.
			// A new requestrules.ForceAddRule() might be the best way.

			return nil, err
		}

		defer func() {
			if retErr != nil || lifespan == common.LifespanSingle {
				p.rules.RemoveRule(userID, newRule.ID)
			}
		}()
	}

	prompt, retErr = p.prompts.Reply(userID, promptID, outcome)
	if retErr != nil {
		return nil, retErr
	}

	if lifespan == common.LifespanSingle {
		return []string{}, nil
	}

	// Apply new rule to outstanding prompts.
	satisfiedPromptIDs, err = p.prompts.HandleNewRule(userID, newRule.Snap, newRule.Interface, newRule.Constraints, newRule.Outcome)
	if err != nil {
		// Should not occur, as outcome and constraints have already been
		// validated. However, it's possible an error could occur if the prompt
		// DB was already closed. This should be an internal error. However, we
		// can't un-send the reply, and this should only be the case if the
		// prompting system is shutting down, so don't actually return an error.
		logger.Noticef("WARNING: error when handling new rule as a result of reply: %v", err)
	}

	return satisfiedPromptIDs, nil
}

// GetRules returns all rules for the user with the given user ID and,
// optionally, only those for the given snap and/or interface.
func (p *Prompting) GetRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	if snap != "" {
		if iface != "" {
			rules := p.rules.RulesForSnapInterface(userID, snap, iface)
			return rules, nil
		}
		rules := p.rules.RulesForSnap(userID, snap)
		return rules, nil
	}
	if iface != "" {
		rules := p.rules.RulesForInterface(userID, iface)
		return rules, nil
	}
	rules := p.rules.Rules(userID)
	return rules, nil
}

// AddRule creates a new rule with the given contents and then checks it against
// outstanding prompts, resolving any prompts which it satisfies.
func (p *Prompting) AddRule(userID uint32, snap string, iface string, constraints *common.Constraints, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (*requestrules.Rule, error) {
	newRule, err := p.rules.AddRule(userID, snap, iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	// Apply new rule to outstanding prompts.
	if _, err = p.prompts.HandleNewRule(userID, newRule.Snap, newRule.Interface, newRule.Constraints, newRule.Outcome); err != nil {
		// Should not occur, as outcome and constraints have already been
		// validated. However, it's possible an error could occur if the prompt
		// DB was already closed. This should be an internal error. However,
		// this should only be the case if the prompting system is shutting
		// down, so don't actually return an error.
		logger.Noticef("WARNING: error when handling new rule as a result of reply: %v", err)
	}
	return newRule, nil
}

// RemoveRules removes all rules for the user with the given user ID and the
// given snap and, optionally, only those for the given interface.
func (p *Prompting) RemoveRules(userID uint32, snap string, iface string) []*requestrules.Rule {
	var removedRules []*requestrules.Rule
	// Already checked that snap != ""
	if iface != "" {
		removedRules = p.rules.RemoveRulesForSnapInterface(userID, snap, iface)
	} else {
		removedRules = p.rules.RemoveRulesForSnap(userID, snap)
	}
	return removedRules
}

// GetRule returns the rule with the given ID for the given user.
func (p *Prompting) GetRule(userID uint32, ruleID string) (*requestrules.Rule, error) {
	rule, err := p.rules.RuleWithID(userID, ruleID)
	return rule, err
}

// PatchRule updates the rule with the given ID using the provided contents.
// Any of the given fields which are empty/nil are not updated in the rule.
func (p *Prompting) PatchRule(userID uint32, ruleID string, constraints *common.Constraints, outcome common.OutcomeType, lifespan common.LifespanType, duration string) (*requestrules.Rule, error) {
	return p.rules.PatchRule(userID, ruleID, constraints, outcome, lifespan, duration)
}

func (p *Prompting) RemoveRule(userID uint32, ruleID string) (*requestrules.Rule, error) {
	rule, err := p.rules.RemoveRule(userID, ruleID)
	return rule, err
}
