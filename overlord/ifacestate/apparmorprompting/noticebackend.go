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

func initializeNoticeBackend(noticeMgr *notices.NoticeManager) (*noticeBackend, error) {
	nextNoticeTimestamp := noticeMgr.NextNoticeTimestamp
	backend := &noticeBackend{}

	now := time.Now()

	path := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "prompt-notices.json")
	if err := backend.promptBackend.initialize(now, nextNoticeTimestamp, path, state.InterfacesRequestsPromptNotice, promptNoticeNamespace); err != nil {
		return nil, err
	}

	path = filepath.Join(dirs.SnapInterfacesRequestsStateDir, "rule-notices.json")
	if err := backend.ruleBackend.initialize(now, nextNoticeTimestamp, path, state.InterfacesRequestsRuleUpdateNotice, ruleNoticeNamespace); err != nil {
		return nil, err
	}

	return backend, nil
}

func (nb *noticeBackend) registerWithManager(noticeMgr *notices.NoticeManager) error {
	// we don't use the validation closure, since notices are derived directly
	// to satisfy validation
	_, err := noticeMgr.RegisterBackend(&nb.promptBackend, state.InterfacesRequestsPromptNotice, promptNoticeNamespace)
	if err != nil {
		// This should never occur
		return fmt.Errorf("cannot register prompting manager as a prompt notice backend")
	}
	_, err = noticeMgr.RegisterBackend(&nb.ruleBackend, state.InterfacesRequestsRuleUpdateNotice, ruleNoticeNamespace)
	if err != nil {
		// This should never occur (if were to, we would need to unregister the prompt backend)
		return fmt.Errorf("cannot register prompting manager as a rule notice backend")
	}

	filter := &state.NoticeFilter{Types: []state.NoticeType{state.InterfacesRequestsPromptNotice}}
	notices := nb.promptBackend.BackendNotices(filter)
	if len(notices) > 0 {
		lastNoticeTimestamp := notices[len(notices)-1].LastRepeated
		noticeMgr.ReportLastNoticeTimestamp(lastNoticeTimestamp)
	}

	filter = &state.NoticeFilter{Types: []state.NoticeType{state.InterfacesRequestsRuleUpdateNotice}}
	notices = nb.ruleBackend.BackendNotices(filter)
	if len(notices) > 0 {
		lastNoticeTimestamp := notices[len(notices)-1].LastRepeated
		noticeMgr.ReportLastNoticeTimestamp(lastNoticeTimestamp)
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
}

func (ntb *noticeTypeBackend) initialize(now time.Time, nextNoticeTimestamp func() time.Time, path string, noticeType state.NoticeType, namespace string) error {
	ntb.lock.Lock()
	defer ntb.lock.Unlock()
	ntb.nextNoticeTimestamp = nextNoticeTimestamp
	ntb.filepath = path
	ntb.noticeType = noticeType
	ntb.namespace = namespace
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
	timestamp := ntb.nextNoticeTimestamp()
	userNotices, ok := ntb.userNotices[userID]
	if !ok {
		userNotices = make([]*state.Notice, 0, 1)
		ntb.userNotices[userID] = userNotices
	}
	// Look through existing notices from most recent to least recent, since
	// recent notices are more likely to re-occur than older notices, and
	// because once we find an expired notices, we know all earlier notices
	// are also expired.
	var notice *state.Notice
	var noticeIndex indexInfo  // existing notice
	var expiredIndex indexInfo // first expired notice
	// Look for existing notice and first expired notice
	for i := len(userNotices) - 1; i >= 0; i-- {
		n := userNotices[i]
		if n.Expired(timestamp) {
			expiredIndex.found = true
			expiredIndex.index = i
			break
		}
		if n.Key == key {
			notice = n
			noticeIndex.found = true
			noticeIndex.index = i
		}
	}
	if !noticeIndex.found {
		notice = &state.Notice{
			ID:            noticeID,
			UserID:        &userID,
			NoticeType:    ntb.noticeType,
			Key:           key,
			FirstOccurred: timestamp,
			ExpireAfter:   defaultExpireAfter,
		}
	}
	notice.LastOccurred = timestamp
	notice.LastRepeated = timestamp
	notice.Occurrences++
	notice.LastData = data

	newNotices := userNotices
	if expiredIndex.found {
		// Discard expired notices but reuse slice to avoid realloc
		newNotices = userNotices[:0]
		if expiredIndex.index < len(userNotices)-1 {
			if noticeIndex.found {
				newNotices = append(newNotices, userNotices[expiredIndex.index+1:noticeIndex.index]...)
				newNotices = append(newNotices, userNotices[noticeIndex.index+1:]...)
			} else {
				newNotices = append(newNotices, userNotices[expiredIndex.index+1:]...)
			}
		}
	} else if noticeIndex.found {
		newNotices = userNotices[:noticeIndex.index]
		if noticeIndex.index < len(userNotices)-1 {
			newNotices = append(newNotices, userNotices[noticeIndex.index+1:]...)
		}
	}
	newNotices = append(newNotices, notice)

	ntb.userNotices[userID] = newNotices
	ntb.save()

	ntb.cond.Broadcast()

	return nil
}

// ntbFilter is a simplified version of state.NoticeFilter which only contains
// information relevant to a noticeTypeBackend.
type ntbFilter struct {
	userID *uint32
	keys   []string
	after  time.Time
	before time.Time
}

// filterNotices filters the given slice of notices, returning only those which
// match the filter. Requires that the notices are sorted by last repeated time.
//
// Assumes that all notices in the slice already apply to the userID in the
// filter.
func (f *ntbFilter) filterNotices(notices []*state.Notice, now time.Time) []*state.Notice {
	if f == nil {
		return notices // XXX: make sure it's safe to not copy
	}
	var filteredNotices []*state.Notice
	// Discard expired notices or those with last repeated timestamp before f.after (if given)
	for i, notice := range notices {
		if notice.Expired(now) {
			continue
		}
		if !f.after.IsZero() && !notice.LastRepeated.After(f.after) {
			continue
		}
		filteredNotices = notices[i:]
		break
	}
	if filteredNotices == nil {
		// Never found a non-expired notice matching after filter
		return []*state.Notice{}
	}
	// Discard notices with last repeated timestamp after f.before (if given)
	if !f.before.IsZero() {
		for i, notice := range filteredNotices {
			if !notice.LastRepeated.Before(f.before) {
				filteredNotices = filteredNotices[:i]
				break
			}
		}
	}

	// Now have non-expired notices matching after/before filters.
	// If filter has no keys, we're done.
	if len(f.keys) == 0 {
		return filteredNotices
	}

	// Look for the keys from the filter
	keyNotices := make([]*state.Notice, 0, len(f.keys))
	for _, notice := range filteredNotices {
		if !sliceContains(f.keys, notice.Key) {
			continue
		}
		keyNotices = append(keyNotices, notice)
		if len(keyNotices) == len(f.keys) {
			break
		}
	}
	return keyNotices
}

// simplifyFilter creates a new simplified filter with only the information
// relevant to this backend. If no notices can match this backend, returns false.
func (ntb *noticeTypeBackend) simplifyFilter(filter *state.NoticeFilter) (simplified *ntbFilter, matchPossible bool) {
	if len(filter.Types) > 0 && !sliceContains(filter.Types, ntb.noticeType) {
		return nil, false
	}
	var keys []string
	if len(filter.Keys) > 0 {
		keys = make([]string, 0, len(filter.Keys))
		sawViableKey := false
		for _, key := range filter.Keys {
			if _, err := prompting.IDFromString(key); err != nil {
				continue
			}
			keys = append(keys, key)
			sawViableKey = true
		}
		if !sawViableKey {
			return nil, false
		}
	}
	if !filter.Before.IsZero() && !filter.After.IsZero() && (filter.After.Equal(filter.Before) || filter.After.After(filter.Before)) {
		// No possible timestamp can satisfy both After and Before filters
		return nil, false
	}
	simplified = &ntbFilter{
		userID: filter.UserID,
		keys:   keys,
		after:  filter.After,
		before: filter.Before,
	}
	return simplified, true
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
		return []*state.Notice{}
	}
	ntb.lock.RLock()
	defer ntb.lock.RUnlock()
	now := time.Now()
	return ntb.doNotices(simplifiedFilter, now)
}

// The caller must hold the backend lock for reading and must not mutate the
// data within the returned slice.
func (ntb *noticeTypeBackend) doNotices(filter *ntbFilter, now time.Time) []*state.Notice {
	if filter.userID != nil {
		userNotices, ok := ntb.userNotices[*filter.userID]
		if !ok {
			return []*state.Notice{}
		}
		return filter.filterNotices(userNotices, now)
	}
	// We'll be combining notices from multiple users, so make sure all notices
	// are copied into a new slice before sorting.
	notices := []*state.Notice{}
	for _, userNotices := range ntb.userNotices {
		notices = append(notices, filter.filterNotices(userNotices, now)...)
	}
	// Since we concatenated notices from multiple users, need to re-sort
	state.SortNotices(notices)
	return notices
}

// BackendNotice returns a single notice by ID, or nil if not found.
func (ntb *noticeTypeBackend) BackendNotice(id string) *state.Notice {
	for _, userNotices := range ntb.userNotices {
		for _, n := range userNotices {
			if n.ID == id {
				return n
			}
		}
	}
	return nil
}

// BackendWaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by last-repeated time.
//
// It waits till there is at least one matching notice or the context is
// cancelled. If there are existing notices that match the filter, WaitNotices
// will return them immediately.
func (ntb *noticeTypeBackend) BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	simplifiedFilter, matchPossible := ntb.simplifyFilter(filter)
	if !matchPossible {
		// XXX: if a match is not possible, should this return an empty list,
		// or just wait until the context is cancelled?
		return []*state.Notice{}, nil
	}
	ntb.lock.RLock()
	defer ntb.lock.RUnlock()
	now := time.Now()
	notices := ntb.doNotices(simplifiedFilter, now)
	if len(notices) > 0 {
		return notices, nil
	}

	// When the context is done/cancelled, wake up the waiters so that they can
	// check their ctx.Err() and return if they're cancelled.
	//
	// TOXO: replace this with context.AfterFunc once we're on Go 1.21.
	stop := contextAfterFunc(ctx, func() {
		// We need to acquire the cond lock here to be sure that the Broadcast
		// below won't occur before the call to Wait, which would result in a
		// missed signal.
		ntb.cond.L.Lock()
		defer ntb.cond.L.Unlock()

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

		if !simplifiedFilter.before.IsZero() && now.After(simplifiedFilter.before) {
			// Since we just checked with the now timestamp and there were no
			// matching notices, and any new notices must have a later timestamp
			// after simplifiedFilter.before, it's impossible for a new notice
			// to match the filter.
			return []*state.Notice{}, nil
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

// Loads existing notices for this backend from disk.
//
// The caller must ensure that the lock is held for writing.
func (ntb *noticeTypeBackend) load(now time.Time) error {
	f, err := os.Open(ntb.filepath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ntb.userNotices = make(map[uint32][]*state.Notice)
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
	// Prune expired notices
	for user, notices := range saved.UserNotices {
		var nonExpiredIndex indexInfo
		for i, n := range notices {
			if n.Expired(now) {
				continue
			}
			nonExpiredIndex.found = true
			nonExpiredIndex.index = i
			break
		}
		// Reuse existing slice, but shift left to reclaim space from expired notices
		if nonExpiredIndex.found {
			ntb.userNotices[user] = append(notices[:0], notices[nonExpiredIndex.index:]...)
		} else {
			// All were expired, so truncate to length 0, preserving all capacity
			ntb.userNotices[user] = notices[:0]
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

type savedNotices struct {
	UserNotices map[uint32][]*state.Notice `json:"user-notices"`
}
