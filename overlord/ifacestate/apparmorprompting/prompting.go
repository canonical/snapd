package apparmorprompting

import (
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
			return fmt.Errorf("listener is nil, cannot run apparmor prompting")
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
	snap, app, err := common.LabelToSnapApp(req.Label())
	if err != nil {
		// the triggering process is not a snap, so treat apparmor label as both snap and app fields
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
		if yesNo, err := p.rules.IsPathAllowed(userID, snap, app, iface, path, perm); err == nil {
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

	newPrompt, merged := p.prompts.AddOrMerge(userID, snap, app, iface, path, remainingPerms, req)
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

func (p *Prompting) GetPrompts(userID uint32) ([]*requestprompts.Prompt, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	prompts := p.prompts.Prompts(userID)
	return prompts, nil
}

func (p *Prompting) GetPrompt(userID uint32, promptID string) (*requestprompts.Prompt, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	prompt, err := p.prompts.PromptWithID(userID, promptID)
	return prompt, err
}

type PromptReply struct {
	Outcome     common.OutcomeType  `json:"action"`
	Lifespan    common.LifespanType `json:"lifespan"`
	Duration    string              `json:"duration,omitempty"`
	Constraints *common.Constraints `json:"constraints"`
}

func (p *Prompting) PostPrompt(userID uint32, promptID string, reply *PromptReply) ([]string, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	prompt, err := p.prompts.PromptWithID(userID, promptID)
	if err != nil {
		return nil, err
	}
	if _, err := common.ValidateConstraintsOutcomeLifespanDuration(prompt.Interface, reply.Constraints, reply.Outcome, reply.Lifespan, reply.Duration); err != nil {
		return nil, err
	}

	// Check that reply.Constraints matches original requested path.
	// AppArmor is responsible for pre-vetting that all paths which appear
	// in requests from the kernel are allowed by the appropriate
	// interfaces, so we do not assert anything else particular about the
	// reply.Constraints.
	// TODO: Should this be reconsidered?
	matches, err := reply.Constraints.Match(prompt.Constraints.Path)
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, fmt.Errorf("constraints in reply do not match original request: '%v' does not match '%v'; please try again", reply.Constraints, prompt.Constraints)
	}
	contained := reply.Constraints.ContainPermissions(prompt.Constraints.Permissions)
	if !contained {
		return nil, fmt.Errorf("replied permissions do not include all requested permissions: requested %v, replied %v; please try again", prompt.Constraints.Permissions, reply.Constraints.Permissions)
	}

	prompt, err = p.prompts.Reply(userID, promptID, reply.Outcome)
	if err != nil {
		return nil, err
	}

	if reply.Lifespan == common.LifespanSingle {
		return make([]string, 0), nil
	}

	// Create new rule based on the reply.
	newRule, err := p.rules.CreateRule(userID, prompt.Snap, prompt.App, prompt.Interface, reply.Constraints, reply.Outcome, reply.Lifespan, reply.Duration)
	if err != nil {
		// XXX: should only occur if identical constraints to an existing rule
		// with overlapping permissions
		// TODO: extract conflicting permissions, retry CreateRule with
		// conflicting permissions removed
		// TODO: what to do if new reply has different Outcome from previous
		// conflicting rule? Modify old rule to remove conflicting permissions,
		// then re-add new rule? This should probably be built into a version of
		// CreateRule (CreateRuleFromReply ?)
		return nil, err
	}

	// Apply new rule to outstanding prompts.
	satisfiedPromptIDs, err := p.prompts.HandleNewRule(userID, newRule.Snap, newRule.App, newRule.Interface, newRule.Constraints, newRule.Outcome)
	if err != nil {
		return nil, err
	}

	return satisfiedPromptIDs, nil
}

type PostRulesCreateRuleContents struct {
	Snap        string              `json:"snap"`
	App         string              `json:"app"`
	Interface   string              `json:"interface"`
	Constraints *common.Constraints `json:"constraints"`
	Outcome     common.OutcomeType  `json:"outcome"`
	Lifespan    common.LifespanType `json:"lifespan"`
	Duration    string              `json:"duration,omitempty"`
}

type PostRulesRemoveSelectors struct {
	Snap      string `json:"snap"`
	App       string `json:"app,omitempty"`
	Interface string `json:"interface,omitempty"`
}

type PostRulesRequestBody struct {
	Action          string                         `json:"action"`
	CreateRules     []*PostRulesCreateRuleContents `json:"rules,omitempty"`
	RemoveSelectors []*PostRulesRemoveSelectors    `json:"selectors,omitempty"`
}

type PostRuleModifyRuleContents struct {
	Constraints *common.Constraints `json:"constraints,omitempty"`
	Outcome     common.OutcomeType  `json:"outcome,omitempty"`
	Lifespan    common.LifespanType `json:"lifespan,omitempty"`
	Duration    string              `json:"duration,omitempty"`
}

type PostRuleRequestBody struct {
	Action string                      `json:"action"`
	Rule   *PostRuleModifyRuleContents `json:"rule,omitempty"`
}

func (p *Prompting) GetRules(userID uint32, snap string, app string, iface string) ([]*requestrules.Rule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	// Daemon already checked that if app != "" or iface != "", then snap != ""
	if iface != "" {
		if app != "" {
			rules := p.rules.RulesForSnapAppInterface(userID, snap, app, iface)
			return rules, nil
		}
		rules := p.rules.RulesForSnapInterface(userID, snap, iface)
		return rules, nil
	}
	if app != "" {
		rules := p.rules.RulesForSnapApp(userID, snap, app)
		return rules, nil
	}
	if snap != "" {
		rules := p.rules.RulesForSnap(userID, snap)
		return rules, nil
	}
	rules := p.rules.Rules(userID)
	return rules, nil
}

func (p *Prompting) PostRulesCreate(userID uint32, rules []*PostRulesCreateRuleContents) ([]*requestrules.Rule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	createdRules := make([]*requestrules.Rule, 0, len(rules))
	errors := make([]error, 0)
	for _, ruleContents := range rules {
		snap := ruleContents.Snap
		app := ruleContents.App
		iface := ruleContents.Interface
		constraints := ruleContents.Constraints
		outcome := ruleContents.Outcome
		lifespan := ruleContents.Lifespan
		duration := ruleContents.Duration
		newRule, err := p.rules.CreateRule(userID, snap, app, iface, constraints, outcome, lifespan, duration)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		createdRules = append(createdRules, newRule)
		// Apply new rule to outstanding prompts. If error occurs,
		// include it in the list of errors from creating rules.
		if _, err := p.prompts.HandleNewRule(userID, newRule.Snap, newRule.App, newRule.Interface, newRule.Constraints, newRule.Outcome); err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		err := fmt.Errorf("")
		for i, e := range errors {
			err = fmt.Errorf("%w%+v: %v; ", err, rules[i], e)
		}
		return createdRules, err
	}
	return createdRules, nil
}

func (p *Prompting) PostRulesRemove(userID uint32, removeSelectors []*PostRulesRemoveSelectors) ([]*requestrules.Rule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	removedRules := make([]*requestrules.Rule, 0)
	for _, selector := range removeSelectors {
		snap := selector.Snap
		app := selector.App
		iface := selector.Interface
		var rulesToRemove []*requestrules.Rule
		// Already checked that snap != ""
		if iface != "" {
			if app != "" {
				rulesToRemove = p.rules.RulesForSnapAppInterface(userID, snap, app, iface)
			} else {
				rulesToRemove = p.rules.RulesForSnapInterface(userID, snap, iface)
			}
		} else if app != "" {
			rulesToRemove = p.rules.RulesForSnapApp(userID, snap, app)
		} else {
			rulesToRemove = p.rules.RulesForSnap(userID, snap)
		}
		for _, rule := range rulesToRemove {
			removedRule, err := p.rules.RemoveRule(userID, rule.ID)
			if err != nil {
				continue
			}
			removedRules = append(removedRules, removedRule)
		}
	}
	return removedRules, nil
}

func (p *Prompting) GetRule(userID uint32, ruleID string) (*requestrules.Rule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	rule, err := p.rules.RuleWithID(userID, ruleID)
	return rule, err
}

func (p *Prompting) PostRuleModify(userID uint32, ruleID string, contents *PostRuleModifyRuleContents) (*requestrules.Rule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	constraints := contents.Constraints
	outcome := contents.Outcome
	lifespan := contents.Lifespan
	duration := contents.Duration
	rule, err := p.rules.ModifyRule(userID, ruleID, constraints, outcome, lifespan, duration)
	return rule, err
}

func (p *Prompting) PostRuleRemove(userID uint32, ruleID string) (*requestrules.Rule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	rule, err := p.rules.RemoveRule(userID, ruleID)
	return rule, err
}
