package requestprompts

import (
	"errors"
	"reflect"
	"sync"

	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
)

var ErrPromptIDNotFound = errors.New("no prompt with the given ID found for the given user")
var ErrUserNotFound = errors.New("no prompts found for the given user")

type Prompt struct {
	ID           string              `json:"id"`
	Timestamp    string              `json:"timestamp"`
	Snap         string              `json:"snap"`
	App          string              `json:"app"`
	Interface    string              `json:"interface"`
	Constraints  *promptConstraints  `json:"constraints"`
	listenerReqs []*listener.Request `json:"-"`
}

type promptConstraints struct {
	Path                 string                  `json:"path"`
	Permissions          []common.PermissionType `json:"permissions"`
	AvailablePermissions []common.PermissionType `json:"available-permissions"`
}

func (pc *promptConstraints) Equals(other *promptConstraints) bool {
	// XXX: should AvailablePermissions be compared?
	return pc.Path == other.Path && reflect.DeepEqual(pc.Permissions, other.Permissions)
}

func (pc *promptConstraints) Matches(pathPattern string) (bool, error) {
	// Only possible error is ErrBadPattern
	return common.PathPatternMatches(pathPattern, pc.Path)
}

func (pc *promptConstraints) subtractPermissions(permissions []common.PermissionType) bool {
	modified := false
	newPermissions := make([]common.PermissionType, 0, len(pc.Permissions))
OUTER_PERMS:
	for _, perm := range pc.Permissions {
		for _, omit := range permissions {
			if omit == perm {
				modified = true
				continue OUTER_PERMS
			}
		}
		newPermissions = append(newPermissions, perm)
	}
	pc.Permissions = newPermissions
	return modified
}

type userPromptDB struct {
	ByID map[string]*Prompt
}

type PromptDB struct {
	PerUser map[uint32]*userPromptDB
	mutex   sync.Mutex
	// Function to issue a notice for a change in a prompt
	notifyPrompt func(userID uint32, promptID string, options *state.AddNoticeOptions) error
}

func New(notifyPrompt func(userID uint32, promptID string, options *state.AddNoticeOptions) error) *PromptDB {
	return &PromptDB{
		PerUser:      make(map[uint32]*userPromptDB),
		notifyPrompt: notifyPrompt,
	}
}

// Creates, adds, and returns a new prompt with the given parameters.
//
// If the parameters exactly match an existing prompt, merge it with that
// existing prompt instead, and do not add a new prompt. If a new prompt was
// added, returns the new prompt and false, indicating the prompt was not
// merged. If it was merged with an identical existing prompt, returns the
// existing prompt and true.
func (pdb *PromptDB) AddOrMerge(user uint32, snap string, app string, iface string, path string, permissions []common.PermissionType, listenerReq *listener.Request) (*Prompt, bool) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.PerUser[user]
	if !exists {
		pdb.PerUser[user] = &userPromptDB{
			ByID: make(map[string]*Prompt),
		}
		userEntry = pdb.PerUser[user]
	}

	constraints := &promptConstraints{
		Path:                 path,
		Permissions:          permissions,
		AvailablePermissions: common.AllPermissions,
	}

	// Search for an identical existing prompt, merge if found
	for _, prompt := range userEntry.ByID {
		if prompt.Snap == snap && prompt.App == app && prompt.Interface == iface && prompt.Constraints.Equals(constraints) {
			prompt.listenerReqs = append(prompt.listenerReqs, listenerReq)
			return prompt, true
		}
	}

	id, timestamp := common.NewIDAndTimestamp()
	prompt := &Prompt{
		ID:           id,
		Timestamp:    timestamp,
		Snap:         snap,
		App:          app,
		Interface:    iface,
		Constraints:  constraints,
		listenerReqs: []*listener.Request{listenerReq},
	}
	userEntry.ByID[id] = prompt
	pdb.notifyPrompt(user, id, nil)
	return prompt, false
}

func (pdb *PromptDB) Prompts(user uint32) []*Prompt {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.PerUser[user]
	if !exists {
		return make([]*Prompt, 0)
	}
	prompts := make([]*Prompt, 0, len(userEntry.ByID))
	for _, prompt := range userEntry.ByID {
		prompts = append(prompts, prompt)
	}
	return prompts
}

func (pdb *PromptDB) PromptWithID(user uint32, id string) (*Prompt, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.PerUser[user]
	if !exists {
		return nil, ErrUserNotFound
	}
	prompt, exists := userEntry.ByID[id]
	if !exists {
		return nil, ErrPromptIDNotFound
	}
	return prompt, nil
}

// Reply resolves the prompt with the given ID using the given outcome.
func (pdb *PromptDB) Reply(user uint32, id string, outcome common.OutcomeType) (*Prompt, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.PerUser[user]
	if !exists || len(userEntry.ByID) == 0 {
		return nil, ErrUserNotFound
	}
	prompt, exists := userEntry.ByID[id]
	if !exists {
		return nil, ErrPromptIDNotFound
	}
	var outcomeBool bool
	switch outcome {
	case common.OutcomeAllow:
		outcomeBool = true
	case common.OutcomeDeny:
		outcomeBool = false
	default:
		return nil, common.ErrInvalidOutcome
	}
	for _, listenerReq := range prompt.listenerReqs {
		if err := sendReply(listenerReq, outcomeBool); err != nil {
			return nil, err
		}
	}
	delete(userEntry.ByID, id)
	pdb.notifyPrompt(user, id, nil)
	return prompt, nil
}

var sendReply = func(listenerReq *listener.Request, reply interface{}) error {
	return listenerReq.Reply(reply)
}

// If any existing prompts are satisfied by the given rule, send the decision
// along their respective channels, and return their IDs.
func (pdb *PromptDB) HandleNewRule(user uint32, snap string, app string, iface string, pathPattern string, outcome common.OutcomeType, permissions []common.PermissionType) ([]string, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	var outcomeBool bool
	switch outcome {
	case common.OutcomeAllow:
		outcomeBool = true
	case common.OutcomeDeny:
		outcomeBool = false
	default:
		return nil, common.ErrInvalidOutcome
	}
	var satisfiedPromptIDs []string
	userEntry, exists := pdb.PerUser[user]
	if !exists {
		return satisfiedPromptIDs, nil
	}
	for id, prompt := range userEntry.ByID {
		if !(prompt.Snap == snap && prompt.App == app && prompt.Interface == iface) {
			continue
		}
		matched, err := prompt.Constraints.Matches(pathPattern)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		modified := prompt.Constraints.subtractPermissions(permissions)
		if !modified {
			continue
		}
		pdb.notifyPrompt(user, id, nil)
		if len(prompt.Constraints.Permissions) > 0 && outcomeBool == true {
			continue
		}
		// all permissions of prompt satisfied, or any permission denied
		for _, listenerReq := range prompt.listenerReqs {
			sendReply(listenerReq, outcomeBool)
		}
		delete(userEntry.ByID, id)
		satisfiedPromptIDs = append(satisfiedPromptIDs, id)
	}
	return satisfiedPromptIDs, nil
}
