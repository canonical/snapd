// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
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

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/snapcore/snapd/logger"
)

var (
	defaultWarningRepeatAfter = time.Hour * 24
	defaultWarningExpireAfter = time.Hour * 24 * 28

	errNoWarningMessage     = errors.New("warning has no message")
	errBadWarningMessage    = errors.New("malformed warning message")
	errNoWarningFirstAdded  = errors.New("warning has no first-added timestamp")
	errNoWarningExpireAfter = errors.New("warning has no expire-after duration")
)

type jsonWarning struct {
	Message     string     `json:"message"`
	FirstAdded  time.Time  `json:"first-added"`
	LastAdded   time.Time  `json:"last-added"`
	LastShown   *time.Time `json:"last-shown,omitempty"`
	ExpireAfter string     `json:"expire-after,omitempty"`
	RepeatAfter string     `json:"repeat-after,omitempty"`
}

type Warning struct {
	// The notice which backs this warning. Notice-specific fields will be
	// extracted and parsed as needed from the lastData map.
	notice *Notice
}

func (w *Warning) String() string {
	if details := w.notice.lastData["details"]; details != "" {
		return details
	}
	return w.notice.key
}

func (w *Warning) firstAdded() time.Time {
	return w.notice.firstOccurred
}

func (w *Warning) lastAdded() time.Time {
	return w.notice.lastRepeated
}

func (w *Warning) lastShown() (time.Time, error) {
	var t time.Time
	lastShownStr, ok := w.notice.lastData["last-shown"]
	if !ok || lastShownStr == "" {
		// no "last-shown"
		return t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, lastShownStr)
	if err != nil {
		return t, fmt.Errorf("cannot parse last-shown timestamp from string: %w", err)
	}
	return t, nil
}

func (w *Warning) expireAfter() time.Duration {
	return w.notice.expireAfter
}

func (w *Warning) repeatAfter() (time.Duration, error) {
	// use "show-after" instead of "repeat-after" to make it clear that this is
	// about "show" in the warnings sense, unrelated to notices repeatAfter.
	showAfterStr, ok := w.notice.lastData["show-after"]
	if !ok || showAfterStr == "" {
		// no "show-after"
		return 0, nil
	}
	showAfter, err := time.ParseDuration(showAfterStr)
	if err != nil {
		return 0, fmt.Errorf("cannot parse show-after duration from string: %w", err)
	}
	return showAfter, nil
}

func (w *Warning) MarshalJSON() ([]byte, error) {
	jw := jsonWarning{
		Message:     w.String(),
		FirstAdded:  w.firstAdded(),
		LastAdded:   w.lastAdded(),
		ExpireAfter: w.expireAfter().String(),
	}
	lastShown, err := w.lastShown()
	if err != nil {
		return nil, err
	}
	if !lastShown.IsZero() {
		jw.LastShown = &lastShown
	}
	repeatAfter, err := w.repeatAfter()
	// XXX: this round-trip is only necessary for validation purposes
	if err != nil {
		return nil, err
	}
	// XXX: the "omitempty" directive in the jsonWarning was and remains always
	// unused, since duration 0 marshals as "0s"
	jw.RepeatAfter = repeatAfter.String()

	return json.Marshal(jw)
}

func (w *Warning) ExpiredBefore(now time.Time) bool {
	return w.notice.Expired(now)
}

func (w *Warning) ShowAfter(t time.Time) bool {
	lastShown, err := w.lastShown()
	if err != nil || lastShown.IsZero() {
		// warning was never shown before; was it added after the cutoff?
		return !w.firstAdded().After(t)
	}
	// Treat invalid "repeat-after" as the same as repeatAfter of 0
	repeatAfter, err := w.repeatAfter()
	if err != nil {
		repeatAfter = 0
	}

	return lastShown.Add(repeatAfter).Before(t)
}

// Warnf records a warning: if it's the first Warning with this
// message it'll be added (with its firstAdded and lastAdded set to the
// current time), otherwise the existing one will have its lastAdded
// updated.
func (s *State) Warnf(template string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(template, args...)
	} else {
		message = template
	}
	s.AddWarning(message, &AddWarningOptions{
		RepeatAfter: defaultWarningRepeatAfter,
	})
}

// AddWarningOptions holds optional parameters for an AddWarning call.
type AddWarningOptions struct {
	// Details gives a more detailed message which will be shown in place of the
	// more generic warning message which can be common across warnings of a
	// given type.
	Details string

	// RepeatAfter defines how long after this warning was last shown we
	// should allow it to repeat. Zero means always repeat.
	RepeatAfter time.Duration

	// Time, if set, overrides time.Now() as the warning lastAdded time.
	Time time.Time
}

// AddWarning records a warning with the specified message and options.
func (s *State) AddWarning(message string, options *AddWarningOptions) {
	if options == nil {
		options = &AddWarningOptions{}
	}

	s.writing()
	// Hold the notices mutex for the duration of the function, since we need
	// to look up the existing notice to preserve the lastShown value.
	// XXX: this wouldn't be the case if we stored lastShown in a dedicated value...
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	addNoticeOptions := &AddNoticeOptions{
		Data: map[string]string{
			"details": options.Details,
		},
		// Always repeat warning notices. The RepeatAfter field in the warning
		// options is really about when a warning should be re-shown to a user,
		// and is unrelated to notice RepeatAfter. We always want to repeat the
		// notice so that the warning data is up to date.
		RepeatAfter: 0,
		ExpireAfter: defaultWarningExpireAfter,
		Time:        options.Time,
	}

	if options.RepeatAfter != 0 {
		addNoticeOptions.Data["show-after"] = options.RepeatAfter.String()
	}

	// Get the existing notice data, if present, to persist the "last-shown" value
	noticeFilter := &NoticeFilter{Types: []NoticeType{WarningNotice}, Keys: []string{message}}
	if existingNotices := s.doNotices(noticeFilter); len(existingNotices) > 0 {
		// Should only be possible to have one notice with a given type and key
		existing := existingNotices[0]
		addNoticeOptions.Data["last-shown"] = existing.lastData["last-shown"]
	}

	if _, err := s.doAddNotice(nil, WarningNotice, message, addNoticeOptions); err != nil {
		// programming error!
		logger.Panicf("internal error, please report: attempted to add invalid warning notice: %v", err)
		return
	}
}

// RemoveWarning removes a warning given its message.
//
// Returns state.ErrNoState if no warning exists with given message.
func (s *State) RemoveWarning(message string) error {
	s.writing()
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	// Remove warning by adding a warning with an ExpireAfter of 1ns
	addNoticeOptions := &AddNoticeOptions{
		RepeatAfter: 0,
		ExpireAfter: time.Nanosecond,
		Time:        timeNow().Add(-time.Nanosecond),
	}
	notice, err := s.doAddNotice(nil, WarningNotice, message, addNoticeOptions)
	if err != nil {
		// programming error!
		logger.Panicf("internal error, please report: attempted to use invalid notice options when removing warning")
		return err
	}
	if notice.occurrences == 1 {
		return ErrNoState
	}
	return nil
}

// AllWarnings returns all the warnings in the system, whether they're
// due to be shown or not. They'll be sorted by lastAdded.
//
// State lock does not need to be held, as the notices mutex ensures
// warnings can be safely read.
func (s *State) AllWarnings() []*Warning {
	s.noticesMu.RLock()
	defer s.noticesMu.RUnlock()
	all, _ := s.allWarningsNow()
	return all
}

// allWarningsNow is a helper function to retrieve all warning notices at the
// current time, whether they're due to be shown or not, convert those notices
// to warnings, and return them along with that time.
//
// The caller must ensure that the notices mutex is locked.
func (s *State) allWarningsNow() ([]*Warning, time.Time) {
	// Get the current time after acquiring noticesMu, so that we're certain to
	// retrieve all notices with a lastRepeated timestamp before the timestamp
	// we return, and it's impossible to add a new notice to the state with a
	// timestamp before now.
	now := time.Now().UTC()
	noticeFilter := &NoticeFilter{Types: []NoticeType{WarningNotice}}
	allWarningNotices := s.doNotices(noticeFilter)

	// "Convert" each warning notice to a Warning
	all := make([]*Warning, len(allWarningNotices))
	for i, n := range allWarningNotices {
		all[i] = &Warning{notice: n}
	}

	return all, now
}

// OkayWarnings marks warnings that were showable at the given time as shown.
func (s *State) OkayWarnings(t time.Time) int {
	t = t.UTC()

	s.writing()
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	warnings, _ := s.allWarningsNow()

	n := 0
	for _, w := range warnings {
		if w.ShowAfter(t) {
			w.notice.lastData["last-shown"] = t.Format(time.RFC3339Nano)
			n++
		}
	}

	return n
}

// PendingWarnings returns the list of warnings to show the user, sorted by
// lastAdded, and a timestamp than can be used to refer to these warnings.
//
// Warnings to show to the user are those that have not been shown before,
// or that have been shown earlier than repeatAfter ago.
//
// State lock does not need to be held, as the notices mutex ensures
// warnings can be safely read.
func (s *State) PendingWarnings() ([]*Warning, time.Time) {
	s.noticesMu.RLock()
	defer s.noticesMu.RUnlock()

	all, now := s.allWarningsNow()

	var toShow []*Warning
	for _, w := range all {
		if !w.ShowAfter(now) {
			continue
		}
		toShow = append(toShow, w)
	}

	return toShow, now
}

// WarningsSummary returns the number of warnings that are ready to be
// shown to the user, and the timestamp of the most recently added
// warning (useful for silencing the warning alerts, and OKing the
// returned warnings).
//
// State lock does not need to be held, as the notices mutex ensures
// warnings can be safely read.
func (s *State) WarningsSummary() (int, time.Time) {
	var last time.Time
	pending, _ := s.PendingWarnings()
	if len(pending) > 0 {
		last = pending[len(pending)-1].lastAdded()
	}
	return len(pending), last
}

// UnshowAllWarnings clears the lastShown timestamp from all the
// warnings. For use in debugging.
func (s *State) UnshowAllWarnings() {
	s.writing()
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()
	all, _ := s.allWarningsNow()
	for _, w := range all {
		delete(w.notice.lastData, "last-shown")
	}
}
