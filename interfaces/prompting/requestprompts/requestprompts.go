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

// Package requestrules provides support for holding outstanding request
// prompts for AppArmor prompting.
package requestprompts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

const (
	// initialTimeout is the duration before which prompts for a given user
	// will expire if there has been no retrieval of prompt details for that
	// user since the previous timeout, or if the user prompt DB was just
	// created.
	initialTimeout = 10 * time.Second
	// activityTimeout is the duration before which prompts for a given user
	// will expire after the most recent retrieval of prompt details for that
	// user.
	activityTimeout = 10 * time.Minute
	// maxOutstandingPromptsPerUser is an arbitrary limit.
	// TODO: review this limit after some usage.
	maxOutstandingPromptsPerUser int = 1000
)

// Prompt contains information about a request for which a user should be
// prompted.
type Prompt struct {
	ID           prompting.IDType
	Timestamp    time.Time
	Snap         string
	PID          int32
	Interface    string
	Constraints  *promptConstraints
	listenerReqs []*listener.Request
}

// jsonPrompt defines the marshalled json structure of a Prompt.
type jsonPrompt struct {
	ID          prompting.IDType       `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Snap        string                 `json:"snap"`
	PID         int32                  `json:"pid"`
	Interface   string                 `json:"interface"`
	Constraints *jsonPromptConstraints `json:"constraints"`
}

// jsonPromptConstraints defines the marshalled json structure of promptConstraints.
type jsonPromptConstraints struct {
	Path                 string   `json:"path"`
	RequestedPermissions []string `json:"requested-permissions"`
	AvailablePermissions []string `json:"available-permissions"`
}

// MarshalJSON marshals the Prompt to JSON.
// TODO: consider having instead a MarshalForClient -> json.RawMessage method
func (p *Prompt) MarshalJSON() ([]byte, error) {
	constraints := &jsonPromptConstraints{
		Path:                 p.Constraints.path,
		RequestedPermissions: p.Constraints.outstandingPermissions,
		AvailablePermissions: p.Constraints.availablePermissions,
	}
	toMarshal := &jsonPrompt{
		ID:          p.ID,
		Timestamp:   p.Timestamp,
		Snap:        p.Snap,
		PID:         p.PID,
		Interface:   p.Interface,
		Constraints: constraints,
	}
	return json.Marshal(toMarshal)
}

func (p *Prompt) sendReply(outcome prompting.OutcomeType) error {
	allow, err := outcome.AsBool()
	if err != nil {
		// This should not occur
		return err
	}
	// Reply with any permissions which were previously allowed
	// If outcome is allow, then reply by allowing all originally-requested
	// permissions. If outcome is deny, only allow permissions which were
	// originally requested but have since been allowed by rules, and deny any
	// outstanding permissions.
	var deniedPermissions []string
	if !allow {
		deniedPermissions = p.Constraints.outstandingPermissions
	}
	allowedPermission := p.Constraints.buildResponse(p.Interface, deniedPermissions)
	return p.sendReplyWithPermission(allowedPermission)
}

func (p *Prompt) sendReplyWithPermission(allowedPermission notify.AppArmorPermission) error {
	for _, listenerReq := range p.listenerReqs {
		if err := sendReply(listenerReq, allowedPermission); err != nil {
			if errors.Is(err, unix.ENOENT) {
				// If err is ENOENT, then notification with the given ID does not
				// exist, so it timed out in the kernel.
				logger.Debugf("kernel returned ENOENT from APPARMOR_NOTIF_SEND (notification probably timed out)")
			} else {
				// Other errors should only occur if reply is malformed, and
				// since these listener requests should be identical, if a
				// reply is malformed for one, it should be malformed for all.
				// Malformed replies should leave the listener request
				// unchanged. Thus, return early.
				return err
			}
		}
	}
	return nil
}

var sendReply = (*listener.Request).Reply

// promptConstraints store the path which was requested, along with three
// lists of permissions: the original permissions associated with the request,
// the outstanding unsatisfied permissions (as rules may satisfy some of the
// permissions from a prompt before the prompt is fully resolved), and the
// available permissions for the interface associated with the prompt, so that
// the client may reply with a broader set of permissions than was originally
// requested.
type promptConstraints struct {
	// path is the path to which the application is requesting access.
	path string
	// outstandingPermissions are the outstanding unsatisfied permissions for
	// which the application is requesting access.
	outstandingPermissions []string
	// availablePermissions are the permissions which are supported by the
	// interface associated with the prompt to which the constraints apply.
	availablePermissions []string
	// originalPermissions preserve the permissions corresponding to the
	// original request. A prompt's permissions may be partially satisfied over
	// time as new rules are added, but we need to keep track of the originally
	// requested permissions so that we can still send back a response to the
	// kernel with all of the originally requested permissions which were
	// explicitly allowed by the user, even if some of those permissions were
	// allowed by rules instead of by the direct reply to the prompt.
	originalPermissions []string
}

// equals returns true if the two prompt constraints apply to the same path and
// were created with the same originally requested permissions. That implies
// that the request which triggered the creation of the two prompts were
// duplicates, the application attempting to do the same action multiple times.
func (pc *promptConstraints) equals(other *promptConstraints) bool {
	if pc.path != other.path || len(pc.originalPermissions) != len(other.originalPermissions) {
		return false
	}
	// Avoid using reflect.DeepEquals to compare []string contents
	for i := range pc.originalPermissions {
		if pc.originalPermissions[i] != other.originalPermissions[i] {
			return false
		}
	}
	return true
}

// applyRuleConstraints modifies the prompt constraints, removing any outstanding
// permissions which are matched by the given rule constraints.
//
// Returns whether the prompt constraints were affected by the rule constraints,
// whether the prompt requires a response (either because all permissions were
// allowed or at least one permission was denied), and the list of any
// permissions which were denied. If an error occurs, it is returned, and the
// other return values can be ignored.
//
// If the path pattern does not match the prompt path, or the permissions in
// the rule constraints do not include any of the outstanding prompt permissions,
// then affectedByRule is false, and no changes are made to the prompt
// constraints.
func (pc *promptConstraints) applyRuleConstraints(constraints *prompting.RuleConstraints) (affectedByRule, respond bool, deniedPermissions []string, err error) {
	pathMatched, err := constraints.Match(pc.path)
	if err != nil {
		// Should not occur, only error is if path pattern is malformed,
		// which would have thrown an error while parsing, not now.
		return false, false, nil, err
	}
	if !pathMatched {
		return false, false, nil, nil
	}

	// Path pattern matched, now check if any permissions match

	newOutstandingPermissions := make([]string, 0, len(pc.outstandingPermissions))
	for _, perm := range pc.outstandingPermissions {
		entry, exists := constraints.Permissions[perm]
		if !exists {
			// Permission not covered by rule constraints, so permission
			// should continue to be in outstandingPermissions.
			newOutstandingPermissions = append(newOutstandingPermissions, perm)
			continue
		}
		affectedByRule = true
		allow, err := entry.Outcome.AsBool()
		if err != nil {
			// This should not occur, as rule constraints are built internally
			return false, false, nil, err
		}
		if !allow {
			deniedPermissions = append(deniedPermissions, perm)
		}
	}
	if !affectedByRule {
		// No permissions matched, so nothing changes, no need to record a
		// notice or send a response.
		return false, false, nil, nil
	}

	pc.outstandingPermissions = newOutstandingPermissions

	if len(pc.outstandingPermissions) == 0 || len(deniedPermissions) > 0 {
		// All permissions allowed or at least one permission denied, so tell
		// the caller to send a response back to the kernel.
		respond = true
	}

	return affectedByRule, respond, deniedPermissions, nil
}

// buildResponse creates a listener response to the receiving prompt constraints
// based on the given interface and list of denied permissions.
//
// The response is the AppArmor permission which should be allowed. This
// corresponds to the originally requested permissions from the prompt
// constraints, except with all denied permissions removed.
func (pc *promptConstraints) buildResponse(iface string, deniedPermissions []string) notify.AppArmorPermission {
	allowedPerms := pc.originalPermissions
	if len(deniedPermissions) > 0 {
		allowedPerms = make([]string, 0, len(pc.originalPermissions)-len(deniedPermissions))
		for _, perm := range pc.originalPermissions {
			if !strutil.ListContains(deniedPermissions, perm) {
				allowedPerms = append(allowedPerms, perm)
			}
		}
	}
	allowedPermission, err := prompting.AbstractPermissionsToAppArmorPermissions(iface, allowedPerms)
	if err != nil {
		// This should not occur, but if so, permission should be set to the
		// empty value for its corresponding permission type.
		logger.Noticef("internal error: cannot convert abstract permissions to AppArmor permissions: %v", err)
	}
	return allowedPermission
}

// Path returns the path associated with the request to which the receiving
// prompt constraints apply.
func (pc *promptConstraints) Path() string {
	return pc.path
}

// OutstandingPermissions returns the outstanding unsatisfied permissions
// associated with the prompt.
func (pc *promptConstraints) OutstandingPermissions() []string {
	return pc.outstandingPermissions
}

// userPromptDB maps prompt IDs to prompts for a single user.
type userPromptDB struct {
	// ids maps from id to the corresponding prompt's index in the prompts list.
	ids map[prompting.IDType]int
	// prompts is the list of prompts which apply to the given user.
	prompts []*Prompt
	// expirationTimer clears the prompts for the given user when it expires.
	expirationTimer timeutil.Timer
}

// get returns the prompt with the given ID from the user prompt DB.
func (udb *userPromptDB) get(id prompting.IDType) (*Prompt, error) {
	index, ok := udb.ids[id]
	if !ok {
		return nil, prompting_errors.ErrPromptNotFound
	}
	return udb.prompts[index], nil
}

// add appends the given prompt to the list of prompts for the user prompt DB
// and maps the ID of the prompt to its index in the list.
func (udb *userPromptDB) add(prompt *Prompt) {
	udb.prompts = append(udb.prompts, prompt)
	index := len(udb.prompts) - 1
	udb.ids[prompt.ID] = index
}

// remove deletes the prompt with the given ID from the user prompt DB and
// returns it.
//
// The prompt is removed from the prompt list my moving the final prompt in the
// list to the index of the removed prompt, truncating the prompt list by one,
// setting the ID of that final prompt to map to the (former) index of the
// removed prompt, and deleting the removed prompt's ID from the map.
func (udb *userPromptDB) remove(id prompting.IDType) (*Prompt, error) {
	index, ok := udb.ids[id]
	if !ok {
		return nil, prompting_errors.ErrPromptNotFound
	}
	prompt := udb.prompts[index]
	// Remove the prompt with the given ID by copying the final prompt in
	// udb.prompts to its index.
	udb.prompts[index] = udb.prompts[len(udb.prompts)-1]
	// Record the ID of the moved prompt now before truncating, in case the
	// prompt to remove is the moved prompt (so nothing was moved).
	movedID := udb.prompts[index].ID
	// Truncate prompts to remove the final element, which was just copied.
	udb.prompts = udb.prompts[:len(udb.prompts)-1]
	// Update the ID-index mapping of the moved prompt.
	udb.ids[movedID] = index
	delete(udb.ids, id)
	return prompt, nil
}

// timeoutCallback is the function which should be called when the expiration
// timer for the receiving user prompt DB expires. This method should never be
// called directly outside of a `time.AfterFunc` call which initializes the
// timer for the user prompt DB when it is first created.
func (udb *userPromptDB) timeoutCallback(pdb *PromptDB, user uint32) {
	pdb.mutex.Lock()
	// We don't defer Unlock() since we may need to manually unlock later in
	// the function in order to record a notice and send a reply without
	// holding the DB lock.

	// If the DB has been closed, do nothing. Thus, there's no need to stop the
	// expiration timer when the DB has been closed, since the timer will fire
	// and do nothing.
	if pdb.isClosed() {
		pdb.mutex.Unlock()
		return
	}
	// Restart expiration timer while holding the lock, so we don't
	// overwrite a newly-set activity timeout with an initial timeout.
	// With the lock held, no activity can occur, so no activity timeout
	// can be set.
	if udb.expirationTimer.Reset(initialTimeout) {
		// Timer was active again, suggesting that some activity caused
		// the timer to be reset at some point between the timer firing
		// and the lock being released and subsequently acquired by this
		// function. So reset the timer to activityTimeout, and do not
		// purge prompts.
		udb.activityResetExpiration()
		pdb.mutex.Unlock()
		return
	}
	expiredPrompts := udb.prompts
	// Clear all outstanding prompts for the user
	udb.prompts = nil
	udb.ids = make(map[prompting.IDType]int) // TODO: clear() once we're on Go 1.21+
	// Unlock now so we can record notices without holding the prompt DB lock
	pdb.mutex.Unlock()
	data := map[string]string{"resolved": "expired"}
	for _, p := range expiredPrompts {
		pdb.notifyPrompt(user, p.ID, data)
		p.sendReply(prompting.OutcomeDeny) // ignore any error, should not occur
	}
}

// activityResetExpiration resets the expiration timer for prompts for the
// receiving user prompt DB. Returns true if the timer had been active, false
// if the timer had expired or been stopped.
func (udb *userPromptDB) activityResetExpiration() bool {
	return udb.expirationTimer.Reset(activityTimeout)
}

// PromptDB stores outstanding prompts in memory and ensures that new prompts
// are created with a unique ID.
type PromptDB struct {
	// The prompt DB is protected by a RWMutex.
	mutex sync.RWMutex
	// maxIDMmap is the byte slice which is memory mapped to the max ID file in
	// order to avoid unnecessary syscalls.
	// If maxIDMmap is closed, then the prompt DB has already been closed.
	maxIDMmap maxidmmap.MaxIDMmap
	// perUser maps UID to the DB of prompts for that user.
	perUser map[uint32]*userPromptDB
	// notifyPrompt is a closure which will be called to record a notice when a
	// prompt is added, merged, modified, or resolved.
	notifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error
}

// New creates and returns a new prompt database.
//
// The given notifyPrompt closure will be called when a prompt is added,
// merged, modified, or resolved. In order to guarantee the order of notices,
// notifyPrompt is called with the prompt DB lock held, so it should not block
// for a substantial amount of time (such as to lock and modify snapd state).
func New(notifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error) (*PromptDB, error) {
	legacyMaxIDFilepath := filepath.Join(dirs.SnapRunDir, "request-prompt-max-id")
	maxIDFilepath := filepath.Join(dirs.SnapInterfacesRequestsRunDir, "request-prompt-max-id")
	if err := os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create interfaces requests run directory: %w", err)
	}
	if !osutil.FileExists(maxIDFilepath) && osutil.FileExists(legacyMaxIDFilepath) {
		// Previous snapd stored max ID file in the snapd run dir, so link it
		// to the new location, so snapd doesn't reuse prompt IDs. Link instead
		// of moving in case snapd reverts and wants to use the old location
		// again.
		if err := osutil.AtomicLink(legacyMaxIDFilepath, maxIDFilepath); err != nil {
			return nil, err
		}
	}
	maxIDMmap, err := maxidmmap.OpenMaxIDMmap(maxIDFilepath)
	if err != nil {
		return nil, err
	}
	pdb := PromptDB{
		perUser:      make(map[uint32]*userPromptDB),
		notifyPrompt: notifyPrompt,
		maxIDMmap:    maxIDMmap,
	}
	return &pdb, nil
}

var timeAfterFunc = func(d time.Duration, f func()) timeutil.Timer {
	return timeutil.AfterFunc(d, f)
}

// AddOrMerge checks if the given prompt contents are identical to an existing
// prompt and, if so, merges with it by adding the given listenerReq to it.
// Otherwise, adds a new prompt with the given contents to the prompt DB.
// If an error occurs, no change is made to the DB.
//
// If the prompt was merged with an identical existing prompt, returns the
// existing prompt and true, indicating it was merged. If a new prompt was
// added, returns the new prompt and false, indicating the prompt was not
// merged.
//
// The caller must ensure that the given permissions are in the order in which
// they appear in the available permissions list for the given interface.
func (pdb *PromptDB) AddOrMerge(metadata *prompting.Metadata, path string, requestedPermissions []string, outstandingPermissions []string, listenerReq *listener.Request) (*Prompt, bool, error) {
	availablePermissions, err := prompting.AvailablePermissions(metadata.Interface)
	if err != nil {
		// Error should be impossible, since caller has already validated that
		// iface is valid, and tests check that all valid interfaces have valid
		// available permissions returned by AvailablePermissions.
		return nil, false, err
	}

	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()

	if pdb.isClosed() {
		return nil, false, prompting_errors.ErrPromptsClosed
	}

	userEntry, ok := pdb.perUser[metadata.User]
	if !ok {
		// New user entry, so create it and set up the expiration timer
		userEntry = &userPromptDB{
			ids: make(map[prompting.IDType]int),
		}
		userEntry.expirationTimer = timeAfterFunc(initialTimeout, func() {
			userEntry.timeoutCallback(pdb, metadata.User)
		})
		pdb.perUser[metadata.User] = userEntry
	}

	constraints := &promptConstraints{
		path:                   path,
		outstandingPermissions: outstandingPermissions,
		availablePermissions:   availablePermissions,
		originalPermissions:    requestedPermissions,
	}

	// Search for an identical existing prompt, merge if found
	for _, prompt := range userEntry.prompts {
		if prompt.Snap == metadata.Snap && prompt.PID == metadata.PID && prompt.Interface == metadata.Interface && prompt.Constraints.equals(constraints) {
			// PID must be identical in order to merge, in case multiple
			// requests come in with different PIDs, so that the client can
			// present the modal dialog on any/all windows associated with the
			// requests. A reply to any prompt which is identical aside from
			// the PID should handle all those otherwise-identical prompts, so
			// long as the lifespan is not "single".
			prompt.listenerReqs = append(prompt.listenerReqs, listenerReq)
			// Although the prompt itself has not changed, re-record a notice
			// to re-notify clients to respond to this request. A client may
			// have replied with a malformed response and not retried after
			// receiving the error, so this notice encourages it to try again
			// if the user retries the operation.
			pdb.notifyPrompt(metadata.User, prompt.ID, nil)
			return prompt, true, nil
		}
	}

	if len(userEntry.prompts) >= maxOutstandingPromptsPerUser {
		logger.Noticef("WARNING: too many outstanding prompts for user %d; auto-denying new one", metadata.User)
		// Deny all permissions which are not already allowed by existing rules
		allowedPermission := constraints.buildResponse(metadata.Interface, constraints.outstandingPermissions)
		sendReply(listenerReq, allowedPermission)
		return nil, false, prompting_errors.ErrTooManyPrompts
	}

	id, _ := pdb.maxIDMmap.NextID() // err must be nil because maxIDMmap is not nil and lock is held
	timestamp := time.Now()
	prompt := &Prompt{
		ID:           id,
		Timestamp:    timestamp,
		Snap:         metadata.Snap,
		PID:          metadata.PID,
		Interface:    metadata.Interface,
		Constraints:  constraints,
		listenerReqs: []*listener.Request{listenerReq},
	}
	userEntry.add(prompt)
	pdb.notifyPrompt(metadata.User, id, nil)
	return prompt, false, nil
}

// Prompts returns a slice of all outstanding prompts for the given user.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (pdb *PromptDB) Prompts(user uint32, clientActivity bool) ([]*Prompt, error) {
	pdb.mutex.RLock()
	defer pdb.mutex.RUnlock()
	if pdb.isClosed() {
		return nil, prompting_errors.ErrPromptsClosed
	}
	userEntry, ok := pdb.perUser[user]
	if !ok || len(userEntry.prompts) == 0 {
		// No prompts for user, but no error
		return nil, nil
	}
	if clientActivity {
		userEntry.activityResetExpiration()
	}
	promptsCopy := make([]*Prompt, len(userEntry.prompts))
	copy(promptsCopy, userEntry.prompts)
	return promptsCopy, nil
}

// PromptWithID returns the prompt with the given ID for the given user.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (pdb *PromptDB) PromptWithID(user uint32, id prompting.IDType, clientActivity bool) (*Prompt, error) {
	pdb.mutex.RLock()
	defer pdb.mutex.RUnlock()
	_, prompt, err := pdb.promptWithID(user, id, clientActivity)
	return prompt, err
}

// promptWithID returns the user entry for the given user and the prompt with
// the given ID for the that user.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
//
// The caller should hold a read (or write) lock on the prompt DB mutex.
func (pdb *PromptDB) promptWithID(user uint32, id prompting.IDType, clientActivity bool) (*userPromptDB, *Prompt, error) {
	if pdb.isClosed() {
		return nil, nil, prompting_errors.ErrPromptsClosed
	}
	userEntry, ok := pdb.perUser[user]
	if !ok {
		return nil, nil, prompting_errors.ErrPromptNotFound
	}
	if clientActivity {
		userEntry.activityResetExpiration()
	}
	prompt, err := userEntry.get(id)
	if err != nil {
		return nil, nil, err
	}
	return userEntry, prompt, nil
}

// Reply resolves the prompt with the given ID using the given outcome by
// sending a reply to all associated listener requests, then removing the
// prompt from the prompt DB.
//
// Records a notice for the prompt, and returns the prompt's former contents.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (pdb *PromptDB) Reply(user uint32, id prompting.IDType, outcome prompting.OutcomeType, clientActivity bool) (*Prompt, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()
	userEntry, prompt, err := pdb.promptWithID(user, id, clientActivity)
	if err != nil {
		return nil, err
	}
	if err = prompt.sendReply(outcome); err != nil {
		return nil, err
	}
	userEntry.remove(id)
	data := map[string]string{"resolved": "replied"}
	pdb.notifyPrompt(user, id, data)
	return prompt, nil
}

// HandleNewRule checks if any existing prompts are satisfied by the given rule
// contents and, if so, sends back a decision to their listener requests.
//
// A prompt is satisfied by the given rule contents if the user, snap,
// interface, and path of the prompt match those of the rule, and all
// outstanding permissions are covered by permissions in the rule constraints
// or at least one of the outstanding permissions is covered by a rule
// permission which has an outcome of "deny".
//
// Records a notice for any prompt which was satisfied, or which had some of
// its permissions satisfied by the rule contents. In the future, only the
// outstanding unsatisfied permissions of a partially-satisfied prompt must be
// satisfied for the prompt as a whole to be satisfied.
//
// Returns the IDs of any prompts which were fully satisfied by the given rule
// contents.
//
// Since rule is new, we don't check the expiration timestamps for any
// permissions, since any permissions with lifespan timespan were validated to
// have a non-zero duration, and we handle this rule as it was at its creation.
func (pdb *PromptDB) HandleNewRule(metadata *prompting.Metadata, constraints *prompting.RuleConstraints) ([]prompting.IDType, error) {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()

	if pdb.isClosed() {
		return nil, prompting_errors.ErrPromptsClosed
	}

	userEntry, ok := pdb.perUser[metadata.User]
	if !ok {
		return nil, nil
	}
	var satisfiedPromptIDs []prompting.IDType
	for _, prompt := range userEntry.prompts {
		if !(prompt.Snap == metadata.Snap && prompt.Interface == metadata.Interface) {
			continue
		}

		affectedByRule, respond, deniedPermissions, err := prompt.Constraints.applyRuleConstraints(constraints)
		if err != nil {
			// Should not occur, only error is if path pattern is malformed,
			// which would have thrown an error while parsing, not now.
			return nil, err
		}
		if !affectedByRule {
			continue
		}
		if !respond {
			// No response necessary, though the prompt constraints were
			// modified, so just record a notice for the prompt.
			pdb.notifyPrompt(metadata.User, prompt.ID, nil)
			continue
		}

		// A response is necessary, so either all permissions were allowed or
		// at least one permission was denied. Construct a response and send it
		// back to the kernel, and record a notice that the prompt was satisfied.
		if len(deniedPermissions) > 0 {
			// At least one permission was denied by new rule, and we want to
			// send a response immediately, so include any outstanding
			// permissions as denied as well.
			//
			// This could be done as part of applyRuleConstraints instead, but
			// it seems semantically clearer to only return the permissions
			// which were explicitly denied by the rule, rather than all
			// outstanding permissions because at least one was denied. It's
			// the prorogative of the caller (this function) to treat the
			// outstanding permissions as denied since we want to send a
			// response without waiting for future rules to satisfy the
			// outstanding permissions.
			deniedPermissions = append(deniedPermissions, prompt.Constraints.outstandingPermissions...)
		}
		// Build and send a response with any permissions which were allowed,
		// either by this new rule or by previous rules.
		allowedPermission := prompt.Constraints.buildResponse(metadata.Interface, deniedPermissions)
		prompt.sendReplyWithPermission(allowedPermission)
		// Now that a response has been sent, remove the rule from the rule DB
		// and record a notice indicating that it has been satisfied.
		userEntry.remove(prompt.ID)
		satisfiedPromptIDs = append(satisfiedPromptIDs, prompt.ID)
		data := map[string]string{"resolved": "satisfied"}
		pdb.notifyPrompt(metadata.User, prompt.ID, data)
	}
	return satisfiedPromptIDs, nil
}

// Close removes all outstanding prompts and records a notice for each one.
//
// This should be called when snapd is shutting down, to notify prompt clients
// that the given prompts are no longer awaiting a reply.
func (pdb *PromptDB) Close() error {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()

	if pdb.isClosed() {
		return prompting_errors.ErrPromptsClosed
	}

	if err := pdb.maxIDMmap.Close(); err != nil {
		return fmt.Errorf("cannot close max ID mmap: %w", err)
	}

	// TODO: if in the future we persist prompts across snapd restarts, we do
	// not want to send {"resolved": "cancelled"} in the notice data.
	data := map[string]string{"resolved": "cancelled"}
	for user, userEntry := range pdb.perUser {
		// No need to explicitly stop the timer, since this is racy with the
		// timer expiring, and since we've already closed the maxIDMmap with
		// the lock held, the callback will acquire the lock, see that the DB
		// has been closed, and immediately return.
		for id := range userEntry.ids {
			pdb.notifyPrompt(user, id, data)
		}
		// There's no need to explicitly send responses back to the kernel
		// here, as the listener will send deny responses for all outstanding
		// listener requests when it is closed, and even if it didn't, the
		// kernel will automatically reclaim unanswered notifications when
		// the notify socket FD is closed, and either roll them over to a new
		// FD or auto-deny them after some time.
	}

	// Clear all outstanding prompts
	pdb.perUser = nil
	return nil
}

// isClosed returns true if the prompt DB is already closed.
//
// The caller must ensure that the DB lock is held.
func (pdb *PromptDB) isClosed() bool {
	return pdb.maxIDMmap.IsClosed()
}

// MockSendReply mocks the function to send a reply back to the listener so
// tests, both for this package and for consumers of this package, can mock
// the listener.
func MockSendReply(f func(listenerReq *listener.Request, allowedPermission notify.AppArmorPermission) error) (restore func()) {
	orig := sendReply
	sendReply = f
	return func() {
		sendReply = orig
	}
}
