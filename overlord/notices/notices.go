// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

// Notices implements the notice management functionality. It provides
// a way to wait for notices (system/daemon events).
package notices

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

var timeNow = time.Now

const (
	// NoticeExpireAfter is the default expiry time for notices. 7 days is currently
	// used as that seems in line with the Pebble setting, and also seems reasonable
	// for the types of notices we produce, and that they are meant for consumption by
	// services.
	NoticeExpireAfter = 7 * 24 * time.Hour
)

type NoticeType string

const (
	// Recorded whenever a change is updated: when it is first spawned or its
	// status was updated.
	NoticeChangeUpdate NoticeType = "change-update"
)

type NoticeOptions struct {
	Key         string
	Data        map[string]string
	repeatAfter time.Duration
}

type Notice struct {
	noticeType NoticeType
	key        string
	lastData   map[string]string

	// The first time one of these notices (type and key combination) occurs.
	firstOccurred time.Time
	// The last time one of these notices occurred. This is updated every time
	// one of these notices occurs.
	lastOccurred time.Time
	// The time this notice was last "repeated". This is set when one of these
	// notices first occurs, and updated when it reoccurs at least
	// repeatAfter after the previous lastRepeated time.
	//
	// Notices and WaitNotices return notices ordered by lastRepeated time, so
	// repeated notices will appear at the end of the returned list.
	lastRepeated time.Time
	// How long after one of these was last repeated should we allow it to repeat.
	repeatAfter time.Duration
	// How long since one of these last occurred until we should drop the notice.
	//
	// The repeatAfter duration must be less than this, because the notice
	// won't be tracked after it expires.
	expireAfter time.Duration
}

func (n *Notice) expired(at time.Time) bool {
	return at.After(n.lastOccurred.Add(n.expireAfter))
}

type NoticeFilter struct {
	// Types are the types of notices that we want to listen for
	Types []NoticeType
	// After is the time-stamp from which we want to receive notices from
	// that matches Types.
	After time.Time
}

func (f *NoticeFilter) hasType(noticeType NoticeType) bool {
	for _, t := range f.Types {
		if t == noticeType {
			return true
		}
	}
	return false
}

func (f *NoticeFilter) matches(notice *Notice) bool {
	if len(f.Types) > 0 && !f.hasType(notice.noticeType) {
		return false
	}
	if !f.After.IsZero() && !notice.lastRepeated.After(f.After) {
		return false
	}
	return true
}

type noticeWaiter struct {
	filter NoticeFilter
	ch     chan *Notice
}

func (w *noticeWaiter) notify(notice *Notice) {
	// We notify on a 'if you have room you get one' basis, so
	// should the waiter not have room (deemed unlikely), we simply
	// drop it. Avoid any kind of blocking behavior, as the state is
	// currently locked, and this might be called as a part of callback.
	select {
	case w.ch <- notice:
		break
	default:
		break
	}
}

type noticeManagerKey struct{}

// NoticeManager takes care of notices related state
type NoticeManager struct {
	state            *state.State
	changeListenerID int
	lastWaiterID     int
	notices          map[string]*Notice
	waiters          map[int]*noticeWaiter
}

func Manager(st *state.State) *NoticeManager {
	nm := &NoticeManager{
		state:   st,
		notices: make(map[string]*Notice),
		waiters: make(map[int]*noticeWaiter),
	}
	st.Lock()
	st.Cache(noticeManagerKey{}, nm)
	st.Unlock()
	return nm
}

// Ensure implements StateManager.Ensure. Required by StateEngine, we
// actually do nothing here.
func (m *NoticeManager) Ensure() error {
	return nil
}

func (m *NoticeManager) StartUp() {
	m.state.Lock()
	defer m.state.Unlock()

	m.changeListenerID = m.state.AddChangeStatusChangedHandler(m.onChangeEvent)
}

func (m *NoticeManager) Stop() {
	m.state.Lock()
	defer m.state.Unlock()

	m.state.RemoveChangeStatusChangedHandler(m.changeListenerID)
}

func (m *NoticeManager) onChangeEvent(chg *state.Change, old, new state.Status) {
	m.addNotice(NoticeChangeUpdate, &NoticeOptions{
		Key: chg.ID(),
		Data: map[string]string{
			"old-status": old.String(),
			"new-status": new.String(),
		},
	})
}

func noticeUniqueKey(noticeType NoticeType, key string) string {
	return fmt.Sprintf("%s-%s", noticeType, key)
}

func updateNotice(notice *Notice, opts *NoticeOptions) bool {
	now := timeNow().UTC()
	var notify bool
	if notice.repeatAfter == 0 || now.After(notice.lastRepeated.Add(notice.repeatAfter)) {
		// Update last repeated time if repeat-after time has elapsed (or is zero)
		notice.lastRepeated = now
		notice.lastData = opts.Data
		notify = true
	}
	return notify
}

func newNotice(noticeType NoticeType, opts *NoticeOptions) *Notice {
	now := timeNow().UTC()
	return &Notice{
		noticeType:    noticeType,
		key:           opts.Key,
		lastData:      opts.Data,
		firstOccurred: now,
		lastOccurred:  now,
		lastRepeated:  now,
		repeatAfter:   opts.repeatAfter,
		expireAfter:   NoticeExpireAfter,
	}
}

func (m *NoticeManager) addNotice(noticeType NoticeType, opts *NoticeOptions) error {
	if opts == nil {
		return fmt.Errorf("internal error: notice options must be provided")
	}

	key := noticeUniqueKey(noticeType, opts.Key)
	notify := true
	if notice := m.notices[key]; notice != nil {
		notify = updateNotice(notice, opts)
	} else {
		m.notices[key] = newNotice(noticeType, opts)
	}
	if notify {
		m.notifyWaiters(m.notices[key])
	}
	return nil
}

func (m *NoticeManager) flattenNotices(filter NoticeFilter) []*Notice {
	now := timeNow().UTC()
	var notices []*Notice
	for _, n := range m.notices {
		if n.expired(now) || !filter.matches(n) {
			continue
		}
		notices = append(notices, n)
	}
	return notices
}

func (m *NoticeManager) noticesMatchingFilter(filter NoticeFilter) []*Notice {
	notices := m.flattenNotices(filter)
	sort.Slice(notices, func(i, j int) bool {
		return notices[i].lastRepeated.Before(notices[j].lastRepeated)
	})
	return notices
}

func (m *NoticeManager) notifyWaiters(notice *Notice) {
	for _, w := range m.waiters {
		if w.filter.matches(notice) {
			w.notify(notice)
		}
	}
}

// addNoticeWaiter adds a notice-waiter with the given filters. Processing
// notices for this waiter stops when the done channel is closed.
func (s *NoticeManager) addNoticeWaiter(filter NoticeFilter) (ch chan *Notice, waiterId int) {
	s.lastWaiterID++
	waiter := &noticeWaiter{
		filter: filter,
		// Create a buffered channel, with room for 16 notices. It's far more
		// than what is probably necessary, but we don't want the waiter to miss
		// anything, even if there should be a short notice burst.
		ch: make(chan *Notice, 16),
	}
	s.waiters[s.lastWaiterID] = waiter
	return waiter.ch, s.lastWaiterID
}

// removeNoticeWaiter removes the notice-waiter with the given ID.
func (s *NoticeManager) removeNoticeWaiter(waiterId int) {
	delete(s.waiters, waiterId)
}

func (m *NoticeManager) wait(ctx context.Context, filter NoticeFilter) ([]*Notice, error) {
	// State is already locked here by the caller, so notices won't be added
	// concurrently.
	notices := m.noticesMatchingFilter(filter)
	if len(notices) > 0 {
		return notices, nil // if there are existing notices, return them right away
	}

	// Add a waiter channel for AddNotice to send to when matching notices arrive.
	ch, waiterId := m.addNoticeWaiter(filter)

	// state will be re-locked when this is called
	defer m.removeNoticeWaiter(waiterId)

	// Unlock state while waiting, to allow new notices to arrive (all state
	// methods expect the caller to have locked the state before the call).
	m.state.Unlock()
	defer m.state.Lock()

	select {
	case notice := <-ch:
		return []*Notice{notice}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func noticeManager(st *state.State, errMsg string) *NoticeManager {
	cached := st.Cached(noticeManagerKey{})
	if cached == nil {
		panic(errMsg)
	}
	return cached.(*NoticeManager)
}

// AddNotice registers a new notice with the notice manager, and notifies any listeners
// waiting for the matching type of notices.
func AddNotice(st *state.State, noticeType NoticeType, opts *NoticeOptions) error {
	nm := noticeManager(st, "internal error: cannot add notices before initialization")
	return nm.addNotice(noticeType, opts)
}

// Notices returns any notices matching the given filter
func Notices(st *state.State, filter NoticeFilter) []*Notice {
	nm := noticeManager(st, "internal error: cannot add notices before initialization")
	return nm.noticesMatchingFilter(filter)
}

// WaitForNotices waits for new notices that match the given filter
func WaitForNotices(st *state.State, ctx context.Context, filter NoticeFilter) ([]*Notice, error) {
	nm := noticeManager(st, "internal error: cannot wait for notices before initialization")
	return nm.wait(ctx, filter)
}
