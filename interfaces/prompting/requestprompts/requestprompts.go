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
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	Interface    string
	Constraints  *promptConstraints
	listenerReqs []*listener.Request
}

// jsonPrompt defines the marshalled json structure of a Prompt.
type jsonPrompt struct {
	ID          prompting.IDType       `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Snap        string                 `json:"snap"`
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
			// Error should only occur if reply is malformed, and since these
			// listener requests should be identical, if a reply is malformed
			// for one, it should be malformed for all. Malformed replies should
			// leave the listener request unchanged. Thus, return early.
			return err
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

	// Remove the request ID mappings now before unlocking the prompt DB
	for _, p := range expiredPrompts {
		for _, listenerReq := range p.listenerReqs {
			delete(pdb.requestIDToPromptID, listenerReq.ID)
		}
	}
	pdb.saveRequestIDPromptIDMapping()

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
	// XXX: this should probably just be a Mutex instead.
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

	requestIDToPromptIDFilepath string

	// requestToPromptID is the mapping from request ID to prompt ID which is
	// kept updated on disk and re-read when snapd restarts, so that we can
	// re-associate each request which is re-received with a prompt with the
	// same ID after snapd restarts.
	requestIDToPromptID map[uint64]prompting.IDType

	// When the prompt DB is created, any existing mappings are loaded from
	// disk. Subsequently, whenever a new request is received, it is checked
	// against the mappings to see if a prompt already existed for the request
	// with that ID, and if so, ensures that the prompt associated with the
	// request has the same ID that it previously had.
	//
	// TODO: handle tough edge cases:
	// - Mapping exists for the request ID to a prompt ID, but the prompt
	//   doesn't currently exist, and there is another prompt which is
	//   identical in content to the new request.
	//   - Simple solution: if a mapping exists for the request ID but a prompt
	//     with that ID doesn't yet exist, create it, and don't bother checking
	//     whether there are other prompts which match.
	//     - Worst case, there are duplicate prompts, not the end of the world.
	//     - XXX: use this solution for now.
	//   - Medium solution: since the new prompt already exists and is fresher,
	//     use it and associate the repeated request with it instead, and clean
	//     up the old prompt by recording a notice that the old prompt has been
	//     cancelled or merged into the new one.
	//   - Complex solution: whichever prompt receives a reply first, apply
	//     that reply to both the original prompt ID and the new prompt ID.
	//     - TODO: keep a map from prompt ID to other prompt IDs of prompts
	//       which are known to be identical, and when a reply is received for
	//       that prompt ID, treat it as a reply to all prompt IDs it maps to.
	// - Mapping exists for the request ID to a prompt ID, and prompt with that
	//   ID exists, but the request content doesn't match the prompt content.
	//   - This should be impossible, since prompt IDs cannot be reused except
	//     via an existing mapping, and requests themselves are identical when
	//     they are re-sent, so the re-created prompt should be the same as the
	//     original.
	// - A request is received and a prompt is created, then snapd restarts,
	//   and before the request is re-received and the prompt re-created, a new
	//   rule is added which matches the request. Then, when the request is
	//   re-received, it is matched by the rule and a response is sent
	//   automatically, without ever reaching the prompt DB to clear up the
	//   prompt ID mapping or record a notice.
	//   - Simple solution: since the prompt won't ever exist again, any reply
	//     will be met with an error, as if e.g. another client replied to it.
	//     The mapping between request ID and prompt ID will continue to exist
	//     until the machine reboots, then it's cleaned up. No real harm done.
	//   - Robust solution: explicitly clean up any mapping for the given
	//     request ID when the request is sent a reply by the manager before
	//     reaching the requestprompts backend, via a CleanupRequest method of
	//     some sort. Keep track of how many mappings exist to every given
	//     prompt ID, and once all the requests mapping to it have been cleaned
	//     up, record a notice that the prompt has been resolved.
	//     - TODO: have map from prompt ID to count of request IDs associated
	//       with it, and have a CleanupRequest method on the prompt DB which
	//       the manager calls when it sends a response to the kernel without
	//       creating a prompt through the prompt DB.
	// - A prompt exists for a given request, then snapd restarts, then a reply
	//   is received before snapd has re-received the request from the client,
	//   so there's no prompt yet for the reply to apply to.
	//   - Bad solution: return an error to the client, since the request could
	//     have timed out in the kernel. Worst case, the request is re-received
	//     later and an identical request gets re-sent.
	//   - Simple partial solution: via the stored mapping, we know the request
	//     IDs associated with the prompt for which the reply is intended, and
	//     as long as the listener is registered, we're able to reply to it,
	//     even before we re-receive the request again.
	//     - Problem is we don't have the request, so we don't know which
	//       permissions were exactly requested, so we can't actually send back
	//       a response safely. And the current mechanism to send a reply is
	//       calling a method on the Request, which we don't have.
	//   - Complicated solution: record the notice that the prompt has been
	//     replied to and store the reply for later, and as every request which
	//     maps to the prompt is re-received from the kernel, handle it with
	//     the same reply, and once every request is either replied or cleaned
	//     up by the manager, then the reply can be dropped.
	//     - How do we validate that the reply is actually valid, since we know
	//       nothing about the content of the original request or prompt?
	//       - Maybe it's fine, if we re-receive the request and it doesn't
	//         match, then we can re-record a notice for the prompt again and
	//         tell the client to try again. This should not happen much
	//         anyway, outside of custom path patterns not being valid or
	//         matching the originally-requested path, both of which the client
	//         should probably be able to check before attempting to reply
	//         anyway?
	//     - What about if not every request is re-received before snapd
	//       restarts again? We don't want to persist the reply to disk as
	//       well.
	//       - It is exceedingly difficult to end up in this position, so
	//         probably not worth designing around it. If a request persists
	//         across two snapd restarts and a reply is received after the
	//         first, odds are the request has timed out, else it should have
	//         been re-received in the time during which the reply had time to
	//         be received.
	//     - TODO: store a map from prompt ID to allowed permissions (for every
	//       prompt ID known to be identical, according to the first edge case),
	//       and when a request is received which maps to that prompt, send
	//       back the cached response automatically.
	//     - TODO maybe later: store the response on disk as well, since it's
	//       possible that the request will not be re-received until after
	//       snapd restarts again.

	// TODO later

	// Record a map from prompt ID to the IDs of other prompts which have
	// identical contents. If a request was received and a prompt created, then
	// snapd restarted, then another request was received which happened to
	// have the same content as the other request, and a new prompt is created
	// for that request, then the original request is re-received, which both
	// matches the new prompt contents and has a previously-assigned prompt.
	// So record that the prompts are identical, and if a response is received
	// for either one, send the response for the other one as well, and record
	// notices that all have received a reply.
	//identicalPrompts map[prompting.IDType][]prompting.IDType

	// Record the number of outstanding requests for each prompt ID, so that if
	// old requests are handled by a new rule before being re-sent to the
	// prompt DB, the manager can still tell the prompt DB to clean up those
	// requests, and if the final request mapping to a prompt ID is cleaned up,
	// then a notice can be recorded
	//promptRequestCounts map[prompting.IDType]int

	// For each prompt which has at least one outstanding request which has not
	// yet been re-received from the kernel, if that prompt receives a reply,
	// record the derived allowed permissions from that reply so that if/when
	// the request is re-received, the reply is already known and can be sent.
	//cachedResponses map[prompting.IDType]notify.AppArmorPermission
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

		requestIDToPromptIDFilepath: filepath.Join(dirs.SnapRunDir, "request-id-prompt-id-mapping"),
	}

	// Load the previous ID mappings from disk
	if err := pdb.loadRequestIDPromptIDMapping(); err != nil {
		return nil, err
	}

	return &pdb, nil
}

// idMappingJSON is the state which is stored on disk, containing the mapping
// from request ID to prompt ID.
type idMappingJSON struct {
	RequestIDToPromptID map[uint64]prompting.IDType `json:"id-mapping"`
}

// loadRequestIDPromptIDMapping loads from disk the mapping from request ID to
// prompt ID, and sets the prompt DB's map to the result.
//
// This method should only be called once, when the prompt DB is first created.
//
// If the existing mapping does not exist, the map is reset to empty, ready
// for new requests to be added.
//
// If the file exists but cannot be read for some reason, returns an error.
func (pdb *PromptDB) loadRequestIDPromptIDMapping() error {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()

	defer func() {
		if pdb.requestIDToPromptID == nil {
			pdb.requestIDToPromptID = make(map[uint64]prompting.IDType)
		}
	}()

	f, err := os.Open(pdb.requestIDToPromptIDFilepath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cannot open mapping from request ID to prompt ID: %w", err)
	}

	var savedState idMappingJSON
	err = json.NewDecoder(f).Decode(&savedState)
	f.Close() // close now since we're done reading
	if err != nil {
		return fmt.Errorf("cannot read stored mapping from request ID to prompt ID: %w", err)
	}

	pdb.requestIDToPromptID = savedState.RequestIDToPromptID

	return nil
}

// saveRequestIDPromptIDMapping saves to disk the mapping from request ID to
// prompt ID.
//
// This function should be called whenever the mapping between request ID and
// prompt ID changes, such as when a prompt is created for a new request or
// when a prompt receives a reply.
//
// The caller must ensure that the database lock is held.
func (pdb *PromptDB) saveRequestIDPromptIDMapping() error {
	b, err := json.Marshal(idMappingJSON{RequestIDToPromptID: pdb.requestIDToPromptID})
	if err != nil {
		// Should not occur, marshalling should always succeed
		logger.Noticef("cannot marshal mapping from request ID to prompt ID: %v", err)
		return fmt.Errorf("cannot marshal mapping from request ID to prompt ID: %w", err)
	}
	return osutil.AtomicWriteFile(pdb.requestIDToPromptIDFilepath, b, 0o600, 0)
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

	getNewPromptID := true

	// Check whether there's a mapping from the request ID to a prompt ID
	promptID, ok := pdb.requestIDToPromptID[listenerReq.ID]
	if ok {
		// A mapping exists, but does the prompt currently exist?
		if prompt, err := userEntry.get(promptID); err == nil {
			// The prompt exists. Confirm that the contents match the request.
			if prompt.Snap == metadata.Snap && prompt.Interface == metadata.Interface && prompt.Constraints.equals(constraints) {
				// The prompt matches the request, all is well. Re-add the
				// request to the prompt, re-record a notice, and return.
				prompt.listenerReqs = append(prompt.listenerReqs, listenerReq)
				// Although the prompt itself has not changed, re-record a notice
				// to re-notify clients to respond to this request. A client may
				// have replied with a malformed response and not retried after
				// receiving the error, so this notice encourages it to try again
				// if the user retries the operation.
				pdb.notifyPrompt(metadata.User, prompt.ID, nil)
				return prompt, true, nil
			}
			// Contents don't match, which should not occur.
			//
			// Remove the mapping, since it's no longer valid, and carry on as
			// if there was no mapping in the first place.
			delete(pdb.requestIDToPromptID, listenerReq.ID)
			// We'll save the new mapping state to disk at the end
			getNewPromptID = true
		} else {
			// The prompt doesn't exist, so we'll have to make a new one, but
			// use the same ID as before
			getNewPromptID = false
		}
	}

	defer pdb.saveRequestIDPromptIDMapping()

	// Only search for prompts which match the request if we don't already know
	// the prompt ID which should be associated with the request. This means we
	// might end up with multiple identical prompts.
	//
	// TODO: search for a matching prompt always, and if !getNewPromptID and we
	// find a matching prompt, then re-associate the request with that prompt's
	// ID, but don't record a notice that the old prompt was cancelled, since
	// there may be other requests which map to it which we have yet to
	// re-receive.
	// TODO: keep a record of how many request IDs map to each prompt ID, so we
	// know if this request was the final one, and we can safely record a
	// notice that it was dropped.
	// TODO: keep a record of identical prompts, so that when a reply is
	// received, it can be applied to all identical prompts.
	// TODO: keep a record of the replies themselves, so that if multiple
	// requests are associated with a given prompt, but a reply is received
	// before the requests are re-received, the reply can be used to respond to
	// those requests.
	if getNewPromptID {
		// Search for an identical existing prompt, merge if found
		for _, prompt := range userEntry.prompts {
			if prompt.Snap == metadata.Snap && prompt.Interface == metadata.Interface && prompt.Constraints.equals(constraints) {
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

		// No matching prompt exists, so we must create a new one.
		// Make sure we don't already have too many.
		if len(userEntry.prompts) >= maxOutstandingPromptsPerUser {
			logger.Noticef("WARNING: too many outstanding prompts for user %d; auto-denying new one", metadata.User)
			// Deny all permissions which are not already allowed by existing rules
			allowedPermission := constraints.buildResponse(metadata.Interface, constraints.outstandingPermissions)
			sendReply(listenerReq, allowedPermission)
			return nil, false, prompting_errors.ErrTooManyPrompts
		}

		// We'll be creating a new prompt and need a new prompt ID, so get it.
		promptID, _ = pdb.maxIDMmap.NextID() // err must be nil because maxIDMmap is not nil and lock is held

		// Now that we have the new ID, map the request ID to the prompt ID and
		// save the mapping to disk.
		pdb.requestIDToPromptID[listenerReq.ID] = promptID
		pdb.saveRequestIDPromptIDMapping() // TODO: maybe handle error, but it shouldn't occur
	}

	timestamp := time.Now()
	prompt := &Prompt{
		ID:           promptID,
		Timestamp:    timestamp,
		Snap:         metadata.Snap,
		Interface:    metadata.Interface,
		Constraints:  constraints,
		listenerReqs: []*listener.Request{listenerReq},
	}
	userEntry.add(prompt)
	pdb.notifyPrompt(metadata.User, promptID, nil)
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

	for _, listenerReq := range prompt.listenerReqs {
		delete(pdb.requestIDToPromptID, listenerReq.ID)
	}
	pdb.saveRequestIDPromptIDMapping() // error should not occur

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

	needToSave := false
	defer func() {
		if needToSave {
			pdb.saveRequestIDPromptIDMapping()
		}
	}()

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

		// Remove the ID mappings for the requests associated with the prompt.
		for _, listenerReq := range prompt.listenerReqs {
			delete(pdb.requestIDToPromptID, listenerReq.ID)
		}
		needToSave = true

		satisfiedPromptIDs = append(satisfiedPromptIDs, prompt.ID)
		data := map[string]string{"resolved": "satisfied"}
		pdb.notifyPrompt(metadata.User, prompt.ID, data)
	}
	return satisfiedPromptIDs, nil
}

// Close removes all outstanding prompts and closes the max ID mmap.
//
// This should be called when snapd is shutting down.
//
// When snapd restarts, it should re-open the max ID mmap and load the mappings
// from request ID to prompt ID from disk, so that prompts can be re-created
// with the same IDs as were previously associated with the requests when they
// are re-received from the kernel.
func (pdb *PromptDB) Close() error {
	pdb.mutex.Lock()
	defer pdb.mutex.Unlock()

	if pdb.isClosed() {
		return prompting_errors.ErrPromptsClosed
	}

	if err := pdb.maxIDMmap.Close(); err != nil {
		return fmt.Errorf("cannot close max ID mmap: %w", err)
	}

	if err := pdb.saveRequestIDPromptIDMapping(); err != nil {
		return fmt.Errorf("cannot save mapping from request ID to prompt ID: %w", err)
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
