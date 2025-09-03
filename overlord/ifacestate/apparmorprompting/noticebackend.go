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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/overlord/state"
)

const (
	promptNoticeNamespace = "prompt"
	ruleNoticeNamespace   = "rule"
	defaultExpireAfter    = 24 * time.Hour
)

// noticeBackends manages notice backends related to prompting.
type noticeBackends struct {
	promptBackend *noticeTypeBackend
	ruleBackend   *noticeTypeBackend
}

func newNoticeBackends(noticeMgr *notices.NoticeManager) (*noticeBackends, error) {
	nextNoticeTimestamp := noticeMgr.NextNoticeTimestamp

	now := time.Now()

	if err := os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create interfaces requests run directory: %w", err)
	}

	path := filepath.Join(dirs.SnapInterfacesRequestsRunDir, "prompt-notices.json")
	promptNoticeBackend, err := newNoticeTypeBackend(now, nextNoticeTimestamp, path, state.InterfacesRequestsPromptNotice, promptNoticeNamespace)
	if err != nil {
		return nil, err
	}

	path = filepath.Join(dirs.SnapInterfacesRequestsRunDir, "rule-notices.json")
	ruleNoticeBackend, err := newNoticeTypeBackend(now, nextNoticeTimestamp, path, state.InterfacesRequestsRuleUpdateNotice, ruleNoticeNamespace)
	if err != nil {
		return nil, err
	}

	backends := &noticeBackends{
		promptBackend: promptNoticeBackend,
		ruleBackend:   ruleNoticeBackend,
	}

	return backends, nil
}

func (nb *noticeBackends) registerWithManager(noticeMgr *notices.NoticeManager) error {
	drainNotices := true
	for _, bknd := range []*noticeTypeBackend{nb.promptBackend, nb.ruleBackend} {
		// We don't use the validation closure, since notices are produced
		// directly to satisfy validation.
		_, drainedNotices, err := noticeMgr.RegisterBackend(bknd, bknd.noticeType, bknd.namespace, drainNotices)
		if err != nil {
			// This should never occur
			return fmt.Errorf("cannot register prompting manager as a %s notice backend", bknd.namespace)
		}
		// Drained notices should only occur the first time snapd starts after
		// refreshing to a new release which uses this notice backend. This is
		// a migration to ensure no information is lost when state is no longer
		// responsible for notices of this type.
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
				logger.Noticef("WARNING: cannot migrate notice from state to %s notice backend: %v", bknd.noticeType, err)
				continue
			}
		}
	}
	return nil
}

// noticeTypeBackend manages notices for a particular notice type.
type noticeTypeBackend struct {
	// rwmu must be held for writing when adding a notice and held for reading
	// when reading notices.
	rwmu sync.RWMutex
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

func newNoticeTypeBackend(now time.Time, nextNoticeTimestamp func() time.Time, path string, noticeType state.NoticeType, namespace string) (*noticeTypeBackend, error) {
	ntb := &noticeTypeBackend{
		nextNoticeTimestamp: nextNoticeTimestamp,
		filepath:            path,
		noticeType:          noticeType,
		namespace:           namespace,
	}
	// Use ntb.rwmu.RLocker() as the cond locker, since that is the lock which
	// is held during BackendWaitNotices(), and thus calling ntb.cond.Wait()
	// will be able to release the lock.
	ntb.cond = sync.NewCond(ntb.rwmu.RLocker())
	if err := ntb.load(now); err != nil {
		return nil, err
	}
	return ntb, nil
}

// addNotice records an occurrence of a notice with the specified user ID, a
// key equal to the given prompt/rule ID, and the given data, with notice ID
// and type derived from the receiver.
func (ntb *noticeTypeBackend) addNotice(userID uint32, id prompting.IDType, data map[string]string) error {
	ntb.rwmu.Lock()
	defer ntb.rwmu.Unlock()
	key := id.String()
	noticeID := fmt.Sprintf("%s-%s", ntb.namespace, key)

	var noticeBackup state.Notice
	userNotices, notice, existing, existingIndex, err := ntb.searchExistingNotices(userID, noticeID)
	if err != nil {
		return err
	}

	// Now that errors can't occur (other than save error), get a new unique
	// timestamp from the state, which will bump the state's noticeLastTimestamp
	timestamp := ntb.nextNoticeTimestamp()

	// Check if any notices have expired relative to the new timestamp.
	// Since they're sorted, as soon as we see a non-expired notice, bail out.
	// Do this before potentially calling Reoccur on the existing notice, so we
	// see its original timestamp in the sorted slice.
	expiredCount := 0
	for _, n := range userNotices {
		if !n.Expired(timestamp) {
			break
		}
		expiredCount++
	}

	existingExpired := existing && notice.Expired(timestamp)
	if existingExpired {
		// Treat an expired existing notice as if it didn't exist at all
		existing = false
	}
	if existing {
		noticeBackup = *notice // save the original state of the existing notice
		notice.Reoccur(timestamp, data, 0)
	} else {
		notice = state.NewNotice(noticeID, &userID, ntb.noticeType, key, timestamp, data, 0, defaultExpireAfter)
	}

	newUserNotices, getOrigUserNotices := assembleNewUserNotices(userNotices, expiredCount, notice, existing, existingIndex)

	ntb.userNotices[userID] = newUserNotices
	ntb.idToNotice[noticeID] = notice

	if err := ntb.save(); err != nil {
		ntb.userNotices[userID] = getOrigUserNotices()
		if existing {
			*notice = noticeBackup
		} else if !existingExpired {
			delete(ntb.idToNotice, noticeID)
		}
		return fmt.Errorf("cannot add notice to prompting %s backend: %w", ntb.noticeType, err)
	}

	// Now that we've successfully saved, delete the expired notices
	for _, expiredNotice := range userNotices[:expiredCount] {
		delete(ntb.idToNotice, expiredNotice.ID())
	}

	ntb.cond.Broadcast()

	return nil
}

// searchExistingNotice looks up the list of existing notices for the given
// userID and checks whether a notice with the given noticeID already exists.
//
// Returns the slice of existing notices for the given userID. If the notice
// does exist, a pointer to it is returned, along with an existing boolean of
// true and the index at which it occurs in the userNotices slice.
//
// The caller must ensure that the backend mutex is locked.
func (ntb *noticeTypeBackend) searchExistingNotices(userID uint32, noticeID string) (userNotices []*state.Notice, notice *state.Notice, existing bool, existingIndex int, err error) {
	notice, ok := ntb.idToNotice[noticeID]
	if !ok {
		userNotices = ntb.userNotices[userID]
		return userNotices, nil, false, 0, nil
	}

	if existingUserID, ok := notice.UserID(); !ok || existingUserID != userID {
		// This should never occur, since prompting notices always have UserIDs
		// and prompt/rule IDs are globally unique.
		if !ok {
			return nil, nil, false, 0, fmt.Errorf("cannot add %s notice with ID %s for user %d: notice with the same ID already exists without user", ntb.namespace, noticeID, userID)
		}
		return nil, nil, false, 0, fmt.Errorf("cannot add %s notice with ID %s for user %d: notice with the same ID already exists for user %d", ntb.namespace, noticeID, userID, existingUserID)
	}

	userNotices, ok = ntb.userNotices[userID]
	if !ok {
		// This should never occur.
		return nil, nil, false, 0, fmt.Errorf("internal error: notice ID maps to notice with user which doesn't exist in user notices: %v", notice)
	}

	// Find the index of the existing notice with this ID.
	// Since the user notices are sorted by LastRepeated timestamp, and
	// each notice has a unique LastRepeated timestamp, we can use binary
	// search by LastRepeated timestamp.
	// XXX: maybe use slices.BinarySearchFunc instead once on go 1.21+
	existingIndex = sort.Search(len(userNotices), func(i int) bool {
		// Find first index which has a LastRepeated timestamp >= the
		// existing notice, since we're binary searching for that notice.
		return !userNotices[i].LastRepeated().Before(notice.LastRepeated())
	})
	if existingIndex >= len(userNotices) || userNotices[existingIndex] != notice {
		// ID maps to a notice which doesn't actually exist in userNotices.
		// This should never occur.
		return nil, nil, false, 0, fmt.Errorf("internal error: notice ID maps to notice which doesn't exist in user notices: %v not in %v", notice, userNotices)
	}

	return userNotices, notice, true, existingIndex, nil
}

// assembleNewUserNotices returns a new slice of notices by discarding any
// expired notices from the front of the given userNotices, appending the given
// notice to the end of the slice, and if it was an existing notice, shifting
// other non-expired notices to the left to fill its original place.
//
// Returns the newly-assembled slice of notices, along with a closure to return
// the original slice if there is need to roll back the changes.
//
// The slice is reused if possible, and memcpy is avoided unless there is need
// to reallocate a new slice with more capacity.
func assembleNewUserNotices(userNotices []*state.Notice, expiredCount int, notice *state.Notice, existing bool, existingIndex int) (newUserNotices []*state.Notice, restore func() []*state.Notice) {
	// Define basic restore if the original slice isn't mutated
	restore = func() []*state.Notice {
		return userNotices
	}
	// Handle simple cases where original slice doesn't need to be mutated
	if expiredCount == len(userNotices) {
		// All expired (or none to begin with), so return new slice
		newUserNotices = []*state.Notice{notice}
		return newUserNotices, restore
	}
	if !existing {
		newUserNotices = append(userNotices[expiredCount:], notice)
		return newUserNotices, restore
	}
	if existingIndex == len(userNotices)-1 {
		newUserNotices = userNotices[expiredCount:]
		return newUserNotices, restore
	}
	// We now know the notice existed and was not last in the slice, and realloc
	// will not occur since the slice length will not change.
	newUserNotices = append(userNotices[expiredCount:existingIndex], userNotices[existingIndex+1:]...)
	newUserNotices = append(newUserNotices, notice)
	// Original slice was muted, so we need better restore function.
	restore = func() []*state.Notice {
		// Shift the originally-later notices right by one
		copy(userNotices[existingIndex+1:], userNotices[existingIndex:])
		userNotices[existingIndex] = notice
		return userNotices
	}
	return newUserNotices, restore
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
		return nil, true
	}
	if len(filter.Types) > 0 && !slicesContains(filter.Types, ntb.noticeType) {
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
	var filteredNotices []*state.Notice
	// Discard expired notices or those with last repeated timestamp before f.After (if given)
	for i, notice := range notices {
		if notice.Expired(now) {
			continue
		}
		if f != nil && !f.After.IsZero() && !notice.LastRepeated().After(f.After) {
			continue
		}
		filteredNotices = notices[i:]
		break
	}
	if filteredNotices == nil || f == nil {
		// Never found a non-expired notice matching After filter, or there is
		// no filter at all and filteredNotices now has all non-expired notices
		return filteredNotices
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
		if !slicesContains(f.Keys, notice.Key()) {
			continue
		}
		keyNotices = append(keyNotices, notice)
		if len(keyNotices) == len(f.Keys) {
			break
		}
	}
	return keyNotices
}

// TODO: remove in favor of slices.Contains once we're on Go 1.21+
func slicesContains[T comparable](haystack []T, needle T) bool {
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
	ntb.rwmu.RLock()
	defer ntb.rwmu.RUnlock()
	now := time.Now()
	return ntb.doNotices(simplifiedFilter, now)
}

// The caller must hold the backend lock for reading and must not mutate the
// data within the returned slice.
func (ntb *noticeTypeBackend) doNotices(filter *ntbFilter, now time.Time) []*state.Notice {
	if filter != nil && filter.UserID != nil {
		userNotices, ok := ntb.userNotices[*filter.UserID]
		if !ok {
			return nil
		}
		return filter.filterNotices(userNotices, now)
	}
	var notices []*state.Notice
	nonEmptyUserNotices := 0
	for _, userNotices := range ntb.userNotices {
		filtered := filter.filterNotices(userNotices, now)
		if len(filtered) == 0 {
			continue
		}
		nonEmptyUserNotices++
		switch nonEmptyUserNotices {
		case 1:
			// Don't copy yet, we don't know if it will be necessary
			notices = filtered
		case 2:
			// Need to copy to a new slice so we can safely append other user
			// notices and sort the end result later.
			newNotices := make([]*state.Notice, len(notices)+len(filtered))
			copy(newNotices, notices)
			copy(newNotices[len(notices):], filtered)
			notices = newNotices
		default:
			notices = append(notices, filtered...)
		}
	}
	if nonEmptyUserNotices > 1 {
		// Since we concatenated notices from multiple users, need to re-sort
		state.SortNotices(notices)
	}
	return notices
}

// BackendNotice returns a single notice by ID, or nil if not found.
func (ntb *noticeTypeBackend) BackendNotice(id string) *state.Notice {
	ntb.rwmu.RLock()
	defer ntb.rwmu.RUnlock()
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
	ntb.rwmu.RLock()
	defer ntb.rwmu.RUnlock()
	now := time.Now()
	notices := ntb.doNotices(simplifiedFilter, now)
	if len(notices) > 0 {
		return notices, nil
	}

	if simplifiedFilter != nil && !simplifiedFilter.BeforeOrAt.IsZero() && simplifiedFilter.BeforeOrAt.Before(now) {
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
		// the cond lock is ntb.rwmu.RLocker(), we need to acquire ntb.rwmu
		// for *writing*, rather than locking ntb.cond.L.
		ntb.rwmu.Lock()
		defer ntb.rwmu.Unlock()

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

		if simplifiedFilter != nil && !simplifiedFilter.BeforeOrAt.IsZero() && now.After(simplifiedFilter.BeforeOrAt) {
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
		ntb.userNotices[user] = notices[:0]
		for i, n := range notices {
			if !n.Expired(now) {
				ntb.userNotices[user] = notices[i:]
				break
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
