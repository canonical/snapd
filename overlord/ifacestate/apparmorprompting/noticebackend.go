// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/overlord/state"
)

const (
	promptNoticeNamespace = "prompt"
	ruleNoticeNamespace   = "rule"
	defaultExpireAfter    = 24 * time.Hour
)

// noticeBackend manages notices related to prompting.
type noticeBackend struct {
	promptBackend noticeTypeBackend
	ruleBackend   noticeTypeBackend
}

func initializeNoticeBackends(noticeMgr *notices.NoticeManager) (*noticeBackend, error) {
	nextNoticeTimestamp := noticeMgr.NextNoticeTimestamp
	backend := &noticeBackend{}

	now := time.Now()

	if err := os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create interfaces requests run directory: %w", err)
	}

	path := filepath.Join(dirs.SnapInterfacesRequestsRunDir, "prompt-notices.json")
	if err := backend.promptBackend.initialize(now, nextNoticeTimestamp, path, state.InterfacesRequestsPromptNotice, promptNoticeNamespace); err != nil {
		return nil, err
	}

	path = filepath.Join(dirs.SnapInterfacesRequestsRunDir, "rule-notices.json")
	if err := backend.ruleBackend.initialize(now, nextNoticeTimestamp, path, state.InterfacesRequestsRuleUpdateNotice, ruleNoticeNamespace); err != nil {
		return nil, err
	}

	return backend, nil
}

func (nb *noticeBackend) registerWithManager(noticeMgr *notices.NoticeManager) error {
	drainNotices := true
	for _, bknd := range []*noticeTypeBackend{&nb.promptBackend, &nb.ruleBackend} {
		// We don't use the validation closure, since notices are produced
		// directly to satisfy validation.
		_, drainedNotices, err := noticeMgr.RegisterBackend(bknd, bknd.noticeType, bknd.namespace, drainNotices)
		if err != nil {
			// This should never occur
			return fmt.Errorf("cannot register prompting manager as a %s notice backend", bknd.namespace)
		}
		for _, notice := range drainedNotices {
			// Re-create each notice in the backend, so no data is lost before
			// a client can receive it. The ID will be different, but the key
			// will be the same.
			userID, _ := notice.UserID() // prompting notices always have UserID
			promptingID, err := prompting.IDFromString(notice.Key())
			if err != nil {
				// Should never occur, as all prompting notices had key set as
				// promptID.String() or ruleID.String()
				continue
			}
			if err = bknd.addNotice(userID, promptingID, notice.LastData()); err != nil {
				// Should never occur, only error would be if two notices with
				// the same key (prompt/rule ID) existed for different users,
				// which should never happen. Or if there's an error saving.
				continue
			}
		}
	}
	return nil
}

// noticeTypeBackend manages notices for a particular notice type.
type noticeTypeBackend struct {
	// lock must be held for writing when adding a notice and held for reading
	// when reading notices.
	lock sync.RWMutex
	// cond is used to broadcast when a new notice is added.
	cond *sync.Cond
	// nextNoticeTimestamp is a closure derived from a notice manager which
	// returns a unique and monotonically increasing next notice timestamp.
	nextNoticeTimestamp func() time.Time
	// filepath is the path where notices for this backend are stored on disk.
	filepath string
	// noticeType is the type of notice managed by this backend.
	noticeType state.NoticeType
	// namespace is the prefix for the IDs of notices managed by this backend.
	namespace string
	// userNotices maps from user ID to the list of notices managed by this
	// backend which are associated with that user. The notices in each list
	// must always remain sorted by last repeated timestamp.
	//
	// This is optimized for the prompting use-case: notice requests for only
	// one user at a time, with the most recent notices being the ones most
	// likely to re-occur.
	userNotices map[uint32][]*state.Notice
	// idToNotice maps from notice ID to the notice itself. This is used to
	// efficiently look up the notice associated with a particular ID, and to
	// ensure that no two notices for different users can have the same ID.
	idToNotice map[string]*state.Notice
}

func (ntb *noticeTypeBackend) initialize(now time.Time, nextNoticeTimestamp func() time.Time, path string, noticeType state.NoticeType, namespace string) error {
	ntb.lock.Lock()
	defer ntb.lock.Unlock()
	ntb.nextNoticeTimestamp = nextNoticeTimestamp
	ntb.filepath = path
	ntb.noticeType = noticeType
	ntb.namespace = namespace
	// Use ntb.lock.RLocker() as the cond lock, since that is the lock which
	// is held during BackendWaitNotices(), and thus calling ntb.cond.Wait()
	// will be able to release the lock.
	ntb.cond = sync.NewCond(ntb.lock.RLocker())
	if err := ntb.load(now); err != nil {
		return err
	}
	return nil
}

type indexInfo struct {
	found bool
	index int
}

// addNotice records an occurrence of a notice with the specified user ID, a
// key equal to the given prompt/rule ID, and the given data, with notice ID
// and type derived from the receiver.
func (ntb *noticeTypeBackend) addNotice(userID uint32, id prompting.IDType, data map[string]string) error {
	ntb.lock.Lock()
	defer ntb.lock.Unlock()
	key := id.String()
	noticeID := fmt.Sprintf("%s-%s", ntb.namespace, key)

	// Retrieve or create the userNotices slice exists for this userID
	userNotices, ok := ntb.userNotices[userID]
	if !ok {
		// XXX: we won't roll back this creation if an error occurs, but it's not a big deal
		userNotices = make([]*state.Notice, 0, 1)
		ntb.userNotices[userID] = userNotices
	}

	// Get a new unique timestamp from the state, which will bump the state's
	// noticeLastTimestamp. Errors below should not occur in practice, so it's
	// fine that this side effect happens before checking for those errors.
	timestamp := ntb.nextNoticeTimestamp()

	// Check if any notices have expired relative to the new timestamp.
	// Since they're sorted, as soon as we see a non-expired notice, bail out.
	// Do this before searching for the existing notice, so we check its
	// original timestamp.
	var expiredIDs []string
	for _, n := range userNotices {
		if !n.Expired(timestamp) {
			break
		}
		expiredIDs = append(expiredIDs, n.ID())
	}

	// Look for an existing notice which matches, otherwise create a new one.
	var existingIndex int
	var noticeBackup state.Notice // if save fails, preserve existing notice contents so it can be rolledback
	var notice *state.Notice
	if existing, ok := ntb.idToNotice[noticeID]; ok {
		// Make sure the notice doesn't already exist for another user
		existingUserID, _ := existing.UserID() // prompting notices always have UserID
		if existingUserID != userID {
			// This should never occur, since prompt/rule IDs are globally unique.
			// (A prompt and rule may have the same ID, but there's no conflict
			// since they use separate backends and notice IDs are namespaced.)
			return fmt.Errorf("cannot add %s notice with ID %s for user %d: notice with same ID already exists for user %d", ntb.namespace, id, userID, existingUserID)
		}

		// Find the index of the existing notice with this ID.
		// Since the user notices are sorted by LastRepeated timestamp, and
		// each notice has a unique LastRepeated timestamp, we can use binary
		// search by LastRepeated timestamp.
		// XXX: maybe use slices.BinarySearchFunc instead once on go 1.21+
		existingIndex = sort.Search(len(userNotices), func(i int) bool {
			// Find first index which has a LastRepeated timestamp >= the
			// existing notice, since we're binary searching for that notice.
			return !userNotices[i].LastRepeated().Before(existing.LastRepeated())
		})
		if existingIndex >= len(userNotices) || userNotices[existingIndex] != existing {
			// ID maps to a notice which doesn't actually exist in userNotices.
			// This should never occur.
			return fmt.Errorf("internal error: notice ID maps to notice which doesn't exist in user notices: %v not in %v", existing, userNotices)
		}
		notice = existing
		noticeBackup = *notice
		notice.Reoccur(timestamp, data, 0)
	} else {
		notice = state.NewNotice(noticeID, &userID, ntb.noticeType, key, timestamp, data, 0, defaultExpireAfter)
	}

	// Assemble new notices slice by discarding any expired notices, removing
	// the existing notice and shifting the others to fill its place, if it
	// exists, and appending the notice to the end.
	newNotices := userNotices
	if len(expiredIDs) > 0 {
		// Discard expired notices but reuse slice to avoid realloc
		newNotices = userNotices[:0]
		if len(expiredIDs) < len(userNotices) {
			if existingIndex >= len(expiredIDs) {
				// Notice already exists and is not expired, so remove it
				// and shift any later notices left
				newNotices = append(newNotices, userNotices[len(expiredIDs):existingIndex]...)
				if existingIndex < len(userNotices)-1 {
					newNotices = append(newNotices, userNotices[existingIndex+1:]...)
				}
			} else {
				newNotices = append(newNotices, userNotices[len(expiredIDs):]...)
			}
		}
	} else if _, ok := ntb.idToNotice[noticeID]; ok {
		// No notices were expired, so simply remove the existing notice and
		// shift any later notices left
		newNotices = userNotices[:existingIndex]
		if existingIndex < len(userNotices)-1 {
			newNotices = append(newNotices, userNotices[existingIndex+1:]...)
		}
	}

	newNotices = append(newNotices, notice)
	ntb.userNotices[userID] = newNotices
	ntb.idToNotice[noticeID] = notice
	if err := ntb.save(); err != nil {
		// Rebuild original userNotices.
		// Since we reused the buffer, it's been mutated, so make a copy of it
		// so we can iteratively rebuild the original information in place.
		mutatedNotices := make([]*state.Notice, len(newNotices))
		copy(mutatedNotices, newNotices)
		restoredNotices := userNotices[:0] // again reuse original slice
		// Re-add all expired notices which were discarded. Their ID mappings still exist.
		for _, expiredID := range expiredIDs {
			restoredNotices = append(restoredNotices, ntb.idToNotice[expiredID])
		}
		// Restore the new notice to its original state (maybe empty)
		*notice = noticeBackup
		// Re-add all the notices except the newest one
		restoredNotices = append(restoredNotices, mutatedNotices[:len(mutatedNotices)-1]...)
		// If the notice existed and was not expired, re-add it.
		if existingIndex >= len(expiredIDs) {
			restoredNotices = append(restoredNotices, notice)
			// Sort so the existing notice ends up in the correct location
			state.SortNotices(restoredNotices)
		}
		ntb.userNotices[userID] = restoredNotices
		// If the notice didn't previously exist, delete it from the ID map
		if noticeBackup.Key() == "" {
			delete(ntb.idToNotice, noticeID)
		}
		return fmt.Errorf("cannot add notice to prompting %s backend: %w", ntb.noticeType, err)
	}

	// Now that we've successfully saved, delete the expired notices
	for _, expiredID := range expiredIDs {
		delete(ntb.idToNotice, expiredID)
	}

	ntb.cond.Broadcast()

	return nil
}

// ntbFilter is a simplified version of state.NoticeFilter which only contains
// information relevant to a noticeTypeBackend.
type ntbFilter struct {
	UserID     *uint32
	Keys       []string
	After      time.Time
	BeforeOrAt time.Time
}

// simplifyFilter creates a new simplified filter with only the information
// relevant to this backend. If no notices can match this backend, returns false.
func (ntb *noticeTypeBackend) simplifyFilter(filter *state.NoticeFilter) (simplified *ntbFilter, matchPossible bool) {
	if filter == nil {
		return &ntbFilter{}, true
	}
	if len(filter.Types) > 0 && !sliceContains(filter.Types, ntb.noticeType) {
		return nil, false
	}
	var keys []string
	if len(filter.Keys) > 0 {
		keys = make([]string, 0, len(filter.Keys))
		sawViableKey := false
		for _, key := range filter.Keys {
			if _, err := prompting.IDFromString(key); err != nil {
				// Key is not a valid prompting ID, so it's imposible for
				// there to be a notice matching it.
				continue
			}
			keys = append(keys, key)
			sawViableKey = true
		}
		if !sawViableKey {
			// There were keys specified in the original filter but none were
			// viable, so it's impossible for notices to match this filter.
			return nil, false
		}
	}
	if !filter.BeforeOrAt.IsZero() && !filter.After.IsZero() && !filter.After.Before(filter.BeforeOrAt) {
		// No possible timestamp can satisfy both After and BeforeOrAt filters
		return nil, false
	}
	simplified = &ntbFilter{
		UserID:     filter.UserID,
		Keys:       keys,
		After:      filter.After,
		BeforeOrAt: filter.BeforeOrAt,
	}
	return simplified, true
}

// filterNotices filters the given slice of notices, returning only those which
// match the filter. Requires that the notices are sorted by last repeated time.
//
// Assumes that all notices in the slice already apply to the UserID in the
// filter.
func (f *ntbFilter) filterNotices(notices []*state.Notice, now time.Time) []*state.Notice {
	if f == nil {
		return notices
	}
	var filteredNotices []*state.Notice
	// Discard expired notices or those with last repeated timestamp before f.After (if given)
	for i, notice := range notices {
		if notice.Expired(now) {
			continue
		}
		if !f.After.IsZero() && !notice.LastRepeated().After(f.After) {
			continue
		}
		filteredNotices = notices[i:]
		break
	}
	if filteredNotices == nil {
		// Never found a non-expired notice matching After filter
		return nil
	}
	// Discard notices with last repeated timestamp after f.BeforeOrAt (if given).
	if !f.BeforeOrAt.IsZero() {
		// Since this filter is not exported over the API, we only expect
		// notices to occur after f.BeforeOrAt if they're racing with a
		// request. So look from newest notice to oldest, and stop looking
		// once we see a notice with timestamp before or at f.BeforeOrAt.
		allAfter := true
		for i := len(filteredNotices) - 1; i >= 0; i-- {
			if filteredNotices[i].LastRepeated().After(f.BeforeOrAt) {
				continue
			}
			allAfter = false
			// Discard all notices with timestamps after this one
			filteredNotices = filteredNotices[:i+1]
			break
		}
		if allAfter {
			// All notices had timestamps after f.BeforeOrAt
			return nil
		}
	}

	// Now have non-expired notices matching After/BeforeOrAt filters.
	// If filter has no keys, we're done.
	if len(f.Keys) == 0 {
		return filteredNotices
	}

	// Look for the keys from the filter
	keyNotices := make([]*state.Notice, 0, len(f.Keys))
	for _, notice := range filteredNotices {
		if !sliceContains(f.Keys, notice.Key()) {
			continue
		}
		keyNotices = append(keyNotices, notice)
		if len(keyNotices) == len(f.Keys) {
			break
		}
	}
	return keyNotices
}

func sliceContains[T comparable](haystack []T, needle T) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// BackendNotices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time.
//
// The caller must not mutate the data within the returned slice.
func (ntb *noticeTypeBackend) BackendNotices(filter *state.NoticeFilter) []*state.Notice {
	simplifiedFilter, matchPossible := ntb.simplifyFilter(filter)
	if !matchPossible {
		return nil
	}
	ntb.lock.RLock()
	defer ntb.lock.RUnlock()
	now := time.Now()
	return ntb.doNotices(simplifiedFilter, now)
}

// The caller must hold the backend lock for reading and must not mutate the
// data within the returned slice.
func (ntb *noticeTypeBackend) doNotices(filter *ntbFilter, now time.Time) []*state.Notice {
	if filter.UserID != nil {
		userNotices, ok := ntb.userNotices[*filter.UserID]
		if !ok {
			return nil
		}
		return filter.filterNotices(userNotices, now)
	}
	// We'll be combining notices from multiple users, so make sure all notices
	// are copied into a new slice before sorting.
	notices := []*state.Notice{}
	for _, userNotices := range ntb.userNotices {
		notices = append(notices, filter.filterNotices(userNotices, now)...)
	}
	if len(ntb.userNotices) > 1 {
		// Since we concatenated notices from multiple users, need to re-sort
		state.SortNotices(notices)
	}
	return notices
}

// BackendNotice returns a single notice by ID, or nil if not found.
func (ntb *noticeTypeBackend) BackendNotice(id string) *state.Notice {
	if noticeEntry, ok := ntb.idToNotice[id]; ok {
		return noticeEntry
	}
	return nil
}

// BackendWaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by last-repeated time.
//
// It waits till there is at least one matching notice, the context is
// cancelled, or the timestamp of the BeforeOrAt filter has passed (if it is
// nonzero). If there are existing notices that match the filter,
// BackendWaitNotices will return them immediately.
func (ntb *noticeTypeBackend) BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	simplifiedFilter, matchPossible := ntb.simplifyFilter(filter)
	if !matchPossible {
		// A match is not possible, so return immediately
		return nil, nil
	}
	ntb.lock.RLock()
	defer ntb.lock.RUnlock()
	now := time.Now()
	notices := ntb.doNotices(simplifiedFilter, now)
	if len(notices) > 0 {
		return notices, nil
	}

	if !simplifiedFilter.BeforeOrAt.IsZero() && simplifiedFilter.BeforeOrAt.Before(now) {
		// No new notices can be added with a timestamp before the BeforeOrAt filter
		return nil, nil
	}

	// When the context is done/cancelled, wake up the waiters so that they can
	// check their ctx.Err() and return if they're cancelled.
	//
	// TODO: replace this with context.AfterFunc once we're on Go 1.21.
	stop := contextAfterFunc(ctx, func() {
		// We need to acquire a lock mutually exclusive with the cond lock here
		// to be sure that the Broadcast below won't occur before the call to
		// Wait, which would result in a missed signal (and deadlock). Since
		// the cond lock is ntb.lock.RLocker(), we need to acquire ntb.lock
		// for *writing*, rather than locking ntb.cond.L.
		ntb.lock.Lock()
		defer ntb.lock.Unlock()

		ntb.cond.Broadcast()
	})
	defer stop()

	for {
		// Wait until a new notice occurs or ctx is cancelled.
		ntb.cond.Wait()

		// If ctx was cancelled, return the error.
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, ctxErr
		}

		now = time.Now()
		// Otherwise, check if there are now matching notices.
		notices = ntb.doNotices(simplifiedFilter, now)
		if len(notices) > 0 {
			return notices, nil
		}

		if !simplifiedFilter.BeforeOrAt.IsZero() && now.After(simplifiedFilter.BeforeOrAt) {
			// Since we just checked with the now timestamp and there were no
			// matching notices, and any new notices must have a later timestamp
			// after simplifiedFilter.BeforeOrAt, it's impossible for a new
			// notice to match the filter.
			return nil, nil
		}
	}
}

// Remove this and just use context.AfterFunc once we're on Go 1.21.
func contextAfterFunc(ctx context.Context, f func()) func() {
	stopCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			f()
		case <-stopCh:
		}
	}()
	stop := func() {
		close(stopCh)
	}
	return stop
}

type savedNotices struct {
	UserNotices map[uint32][]*state.Notice `json:"user-notices"`
}

// Loads existing notices for this backend from disk.
//
// The caller must ensure that the lock is held for writing.
func (ntb *noticeTypeBackend) load(now time.Time) error {
	f, err := os.Open(ntb.filepath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ntb.userNotices = make(map[uint32][]*state.Notice)
			ntb.idToNotice = make(map[string]*state.Notice)
			return nil
		}
		return fmt.Errorf("cannot open %s notices file: %w", ntb.namespace, err)
	}
	defer f.Close()
	var saved savedNotices
	if err = json.NewDecoder(f).Decode(&saved); err != nil {
		return fmt.Errorf("cannot unmarshal %s notices file: %w", ntb.namespace, err)
	}
	ntb.userNotices = make(map[uint32][]*state.Notice)
	ntb.idToNotice = make(map[string]*state.Notice)
	// Prune expired notices
	for user, notices := range saved.UserNotices {
		// Find the index of the last expired notice
		var expiredIndex indexInfo
		for i, n := range notices {
			if !n.Expired(now) {
				break
			}
			expiredIndex.found = true
			expiredIndex.index = i
		}
		if !expiredIndex.found {
			ntb.userNotices[user] = notices
		} else {
			ntb.userNotices[user] = notices[:0]
			if expiredIndex.index < len(notices)-1 {
				// There's at least one non-expired notice, so copy non-expired
				// notices to the start of the buffer
				ntb.userNotices[user] = append(notices[:0], notices[expiredIndex.index+1:]...)
			}
		}
		// Construct ID mappings for these notices
		for _, n := range ntb.userNotices[user] {
			ntb.idToNotice[n.ID()] = n
		}
	}
	return nil
}

// Save notices for this backend to disk.
//
// The caller must ensure that the lock is held.
func (ntb *noticeTypeBackend) save() error {
	b, err := json.Marshal(savedNotices{UserNotices: ntb.userNotices})
	if err != nil {
		// Should not occur, marshalling should always succeed
		return fmt.Errorf("cannot marshal %s notices: %w", ntb.namespace, err)
	}
	return osutil.AtomicWriteFile(ntb.filepath, b, 0o600, 0)
}
