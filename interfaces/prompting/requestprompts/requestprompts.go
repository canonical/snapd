package requestprompts

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
)

// Prompt contains information about a request for which a user should be
// prompted.
type Prompt struct {
	ID           string              `json:"id"`
	Timestamp    time.Time           `json:"timestamp"`
	Snap         string              `json:"snap"`
	Interface    string              `json:"interface"`
	Constraints  *promptConstraints  `json:"constraints"`
	listenerReqs []*listener.Request `json:"-"`
}

// promptConstraints are like prompting.Constraints, but have a "path" field
// instead of a "path-pattern", and include the available permissions for the
// interface corresponding to the prompt.
type promptConstraints struct {
	Path                 string   `json:"path"`
	Permissions          []string `json:"permissions"`
	AvailablePermissions []string `json:"available-permissions"`
}

// equals returns true if the two prompt constraints are identical.
func (pc *promptConstraints) equals(other *promptConstraints) bool {
	if pc.Path != other.Path || len(pc.Permissions) != len(other.Permissions) {
		return false
	}
	// Avoid using reflect.DeepEquals to compare []string contents
	for i := range pc.Permissions {
		if pc.Permissions[i] != other.Permissions[i] {
			return false
		}
	}
	return true
}

// subtractPermissions removes all of the given permissions from the list of
// permissions in the constraints.
func (pc *promptConstraints) subtractPermissions(permissions []string) bool {
	newPermissions := make([]string, 0, len(pc.Permissions))
	for _, perm := range pc.Permissions {
		if !strutil.ListContains(permissions, perm) {
			newPermissions = append(newPermissions, perm)
		}
	}
	if len(newPermissions) != len(pc.Permissions) {
		pc.Permissions = newPermissions
		return true
	}
	return false
}

// userPromptDB maps prompt IDs to prompts for a single user.
type userPromptDB struct {
	ByID map[string]*Prompt
}

// PromptDB stores outstanding prompts and ensures that new prompts are created
// with a unique ID.
type PromptDB struct {
	perUser   map[uint32]*userPromptDB
	maxID     uint64
	maxIDPath string
	mutex     sync.Mutex
	// Function to issue a notice for a change in a prompt
	notifyPrompt func(userID uint32, promptID string) error
}

// New creates and returns a new prompt database.
//
// The given notifyPrompt closure should record a notice of type
// "interfaces-requests-prompt" for the given user with the given
// promptID as its key.
func New(notifyPrompt func(userID uint32, promptID string) error) *PromptDB {
	pdb := PromptDB{
		perUser:      make(map[uint32]*userPromptDB),
		notifyPrompt: notifyPrompt,
	}
	// Importantly, set maxIDPath before attempting to load max ID
	pdb.maxIDPath = filepath.Join(dirs.GlobalRootDir, "/tmp/snapd-request-prompt-max-id")
	// XXX: is /tmp guaranteed to exist, and if not, should we create it here?
	// Otherwise, should a non-/tmp location be used?
	err := pdb.loadMaxID()
	if err != nil {
		// If cannot read max existing prompt ID, start again from 0.
		pdb.maxID = 0
	}
	return &pdb
}

// loadMaxID reads the previous ID from the file at maxIDPath and sets maxID.
//
// If no file exists at maxIDPath, sets maxID to be 0. If another error occurs,
// returns it and does not set maxID.
func (pdb *PromptDB) loadMaxID() error {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()

	target := pdb.maxIDPath
	f, err := os.Open(target)
	if os.IsNotExist(err) {
		pdb.maxID = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read maximum prompt ID: %w", err)
	}
	defer f.Close()

	idBuf := [16]byte{}
	_, err = io.ReadFull(f, idBuf[:])
	if err != nil {
		return fmt.Errorf("cannot read maximum prompt ID: %w", err)
	}

	maxID, err := strconv.ParseUint(string(idBuf[:]), 16, 64)
	if err != nil {
		return fmt.Errorf("cannot parse maximum prompt ID: %w", err)
	}

	pdb.maxID = maxID
	return nil
}

// nextID advances the internal monotonically-increasing maxID integer.
//
// The caller must ensure that the prompt DB mutex is held.
func (pdb *PromptDB) nextID() string {
	pdb.maxID += 1
	padded := pdb.paddedMaxIDString()
	osutil.AtomicWriteFile(pdb.maxIDPath, []byte(padded), 0600, 0)
	return padded
}

// paddedMaxIDString returns a 16-character string corresponding to the current
// maxID. The ID string is the max ID in hexadecimal, padded by leading zeroes.
func (pdb *PromptDB) paddedMaxIDString() string {
	maxIDStr := strconv.FormatUint(pdb.maxID, 16)
	// pad with leading zeros
	padded := "0000000000000000"[:16-len(maxIDStr)] + maxIDStr
	return padded
}

// AddOrMerge checks if the given prompt contents are identical to an existing
// prompt and, if so, merges with it by adding the given listenerReq to it.
// Otherwise, adds a new prompt with the given contents to the prompt DB.
//
// If the prompt was merged with an identical existing prompt, returns the
// existing prompt and true, indicating it was merged. If a new prompt was
// added, returns the new prompt and false, indicating the prompt was not
// merged.
//
// The caller must ensure that the given permissions are in the order in which
// they appear in the available permissions list for the given interface.
func (pdb *PromptDB) AddOrMerge(user uint32, snap string, iface string, path string, permissions []string, listenerReq *listener.Request) (*Prompt, bool) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.perUser[user]
	if !exists {
		pdb.perUser[user] = &userPromptDB{
			ByID: make(map[string]*Prompt),
		}
		userEntry = pdb.perUser[user]
	}

	availablePermissions, _ := prompting.AvailablePermissions(iface)
	// Error should be impossible, since caller has already validated that iface
	// is valid, and tests check that all valid interfaces have valid available
	// permissions returned by AvailablePermissions.

	constraints := &promptConstraints{
		Path:                 path,
		Permissions:          permissions,
		AvailablePermissions: availablePermissions,
	}

	// Search for an identical existing prompt, merge if found
	for _, prompt := range userEntry.ByID {
		if prompt.Snap == snap && prompt.Interface == iface && prompt.Constraints.equals(constraints) {
			prompt.listenerReqs = append(prompt.listenerReqs, listenerReq)
			return prompt, true
		}
	}

	id := pdb.nextID()
	timestamp := time.Now()
	prompt := &Prompt{
		ID:           id,
		Timestamp:    timestamp,
		Snap:         snap,
		Interface:    iface,
		Constraints:  constraints,
		listenerReqs: []*listener.Request{listenerReq},
	}
	userEntry.ByID[id] = prompt
	pdb.notifyPrompt(user, id)
	return prompt, false
}

// Prompts returns a slice of all outstanding prompts.
func (pdb *PromptDB) Prompts(user uint32) []*Prompt {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.perUser[user]
	if !exists {
		return make([]*Prompt, 0)
	}
	prompts := make([]*Prompt, 0, len(userEntry.ByID))
	for _, prompt := range userEntry.ByID {
		prompts = append(prompts, prompt)
	}
	return prompts
}

// PromptWithID returns the prompt with the given ID for the given user.
func (pdb *PromptDB) PromptWithID(user uint32, id string) (*Prompt, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.perUser[user]
	if !exists {
		return nil, fmt.Errorf("cannot find prompts for the given UID: %d", user)
	}
	prompt, exists := userEntry.ByID[id]
	if !exists {
		return nil, fmt.Errorf("cannot find prompt for UID %d with the given ID: %s", user, id)
	}
	return prompt, nil
}

// Reply resolves the prompt with the given ID using the given outcome by
// sending a reply to all associated listener requests, then removing the
// prompt from the prompt DB.
//
// Records a notice for the prompt, and returns the prompt's former contents.
func (pdb *PromptDB) Reply(user uint32, id string, outcome prompting.OutcomeType) (*Prompt, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, exists := pdb.perUser[user]
	if !exists || len(userEntry.ByID) == 0 {
		return nil, fmt.Errorf("cannot find prompts for the given UID: %d", user)
	}
	prompt, exists := userEntry.ByID[id]
	if !exists {
		return nil, fmt.Errorf("cannot find prompt for UID %d with the given ID: %s", user, id)
	}
	allow, err := outcome.IsAllow()
	if err != nil {
		return nil, err
	}
	for _, listenerReq := range prompt.listenerReqs {
		if err := sendReply(listenerReq, allow); err != nil {
			return nil, err
		}
	}
	delete(userEntry.ByID, id)
	pdb.notifyPrompt(user, id)
	return prompt, nil
}

var sendReply = func(listenerReq *listener.Request, reply interface{}) error {
	return listenerReq.Reply(reply)
}

// HandleNewRule checks if any existing prompts are satisfied by the given rule
// contents and, if so, sends back a decision to their listener requests.
//
// A prompt is satisfied by the given rule contents if the user, snap,
// interface, and path of the prompt match those of the rule, and if either the
// outcome is "allow" and all of the prompt's permissions are matched by those
// of the rule contents, or if the outcome is "deny" and any of the permissions
// match.
//
// Records a notice for any prompt which was satisfied, or which had some of
// its permissions satisfied by the rule contents. In the future, only the
// remaining unsatisfied permissions of a partially-satisfied prompt must be
// satisfied for the prompt as a whole to be satisfied.
//
// Returns the IDs of any prompts which were fully satisfied by the given rule
// contents.
func (pdb *PromptDB) HandleNewRule(user uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType) ([]string, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	allow, err := outcome.IsAllow()
	if err != nil {
		return nil, err
	}
	var satisfiedPromptIDs []string
	userEntry, exists := pdb.perUser[user]
	if !exists {
		return satisfiedPromptIDs, nil
	}
	for id, prompt := range userEntry.ByID {
		if !(prompt.Snap == snap && prompt.Interface == iface) {
			continue
		}
		matched, err := constraints.Match(prompt.Constraints.Path)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		modified := prompt.Constraints.subtractPermissions(constraints.Permissions)
		if !modified {
			continue
		}
		pdb.notifyPrompt(user, id)
		if len(prompt.Constraints.Permissions) > 0 && allow == true {
			continue
		}
		// All permissions of prompt satisfied, or any permission denied
		for _, listenerReq := range prompt.listenerReqs {
			sendReply(listenerReq, allow)
		}
		delete(userEntry.ByID, id)
		satisfiedPromptIDs = append(satisfiedPromptIDs, id)
	}
	return satisfiedPromptIDs, nil
}

// CleanUp removes all outstanding prompts and records a notice for each one.
//
// This should be called when snapd is shutting down, to notify prompt clients
// that the given prompts are no longer awaiting a reply.
func (pdb *PromptDB) CleanUp() {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	for user, userEntry := range pdb.perUser {
		for id := range userEntry.ByID {
			pdb.notifyPrompt(user, id)
		}
	}
	// Clear all outstanding prompts
	pdb.perUser = make(map[uint32]*userPromptDB)
}
