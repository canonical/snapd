// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"sort"
	"time"
)

var (
	DefaultRepeatAfter = time.Hour * 24
	DefaultExpireAfter = time.Hour * 24 * 28
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
	// the warning text itself. Only one of these in the system at a time.
	message string
	// the first time one of these messages was created
	firstAdded time.Time
	// the last time one of these was created
	lastAdded time.Time
	// the last time one of these was shown to the user
	lastShown time.Time
	// how much time since one of these was last added should we drop the message
	expireAfter time.Duration
	// how much time since one of these was last shown should we repeat it
	repeatAfter time.Duration
}

func (w *Warning) String() string {
	return w.message
}

func (w *Warning) MarshalJSON() ([]byte, error) {
	jw := jsonWarning{
		Message:     w.message,
		FirstAdded:  w.firstAdded,
		LastAdded:   w.lastAdded,
		ExpireAfter: w.expireAfter.String(),
		RepeatAfter: w.repeatAfter.String(),
	}
	if !w.lastShown.IsZero() {
		jw.LastShown = &w.lastShown
	}

	return json.Marshal(jw)
}

func (w *Warning) UnmarshalJSON(data []byte) error {
	var jw jsonWarning
	err := json.Unmarshal(data, &jw)
	if err != nil {
		return err
	}
	w.message = jw.Message
	w.firstAdded = jw.FirstAdded
	w.lastAdded = jw.LastAdded
	if jw.LastShown != nil {
		w.lastShown = *jw.LastShown
	}
	w.expireAfter, err = time.ParseDuration(jw.ExpireAfter)
	if err != nil {
		return err
	}
	w.repeatAfter, err = time.ParseDuration(jw.RepeatAfter)
	if err != nil {
		return err
	}

	return nil
}

func (w *Warning) Expired(now time.Time) bool {
	return w.lastAdded.Add(w.expireAfter).Before(now)
}

func (w *Warning) IsShowable(t time.Time) bool {
	return (w.lastShown.IsZero() || w.lastShown.Add(w.repeatAfter).Before(t)) && !w.firstAdded.After(t)
}

// flattenWarning loops over the warnings map, and returns all
// warnings therein as a flat list, for serialising.
// Call with the lock held.
func (s *State) flattenWarnings() []*Warning {
	flat := make([]*Warning, 0, len(s.warnings))
	for _, w := range s.warnings {
		flat = append(flat, w)
	}
	return flat
}

// unflattenWarnings takes a flat list of warnings and replaces the
// warning map with them.
// Call with the lock held.
func (s *State) unflattenWarnings(flat []*Warning) {
	s.warnings = make(map[string]*Warning, len(flat))
	for _, w := range flat {
		s.warnings[w.message] = w
	}
}

// AddWarning records a warning: if it's the first Warning with this
// message it'll be added (with its firstAdded and lastAdded set to the
// current time), otherwise the existing one will have its lastAdded
// updated.
func (s *State) AddWarning(message string) {
	s.addWarningFull(Warning{
		message:     message,
		expireAfter: DefaultExpireAfter,
		repeatAfter: DefaultRepeatAfter,
	}, time.Now().UTC())
}

func (s *State) addWarningFull(w Warning, t time.Time) {
	s.writing()

	if s.warnings[w.message] == nil {
		w.firstAdded = t
		s.warnings[w.message] = &w
	}
	s.warnings[w.message].lastAdded = t
}

// DeleteExpired deletes warnings that have expired.
func (s *State) DeleteExpired() int {
	s.writing()

	now := time.Now().UTC()

	n := 0
	for k, w := range s.warnings {
		if w.Expired(now) {
			delete(s.warnings, k)
			n++
		}
	}
	return n
}

type byLastAdded []*Warning

func (a byLastAdded) Len() int           { return len(a) }
func (a byLastAdded) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byLastAdded) Less(i, j int) bool { return a[i].lastAdded.Before(a[j].lastAdded) }

// AllWarnings returns all the warnings in the system, whether they're
// due to be shown or not. They'll be sorted by lastAdded.
func (s *State) AllWarnings() []*Warning {
	s.reading()

	all := s.flattenWarnings()
	sort.Sort(byLastAdded(all))

	return all
}

// OkayWarnings marks warnings that were showable at the given time as shown.
func (s *State) OkayWarnings(t time.Time) int {
	t = t.UTC()
	s.writing()

	n := 0
	for _, w := range s.warnings {
		if w.IsShowable(t) {
			w.lastShown = t
			n++
		}
	}

	return n
}

// WarningsToShow returns the list of warnings to show the user, sorted by
// lastAdded, and a timestamp than can be used to refer to these warnings.
//
// Warnings to show to the user are those that have not been shown before,
// or that have been shown earlier than repeatAfter ago.
func (s *State) WarningsToShow() ([]*Warning, time.Time) {
	s.reading()
	now := time.Now().UTC()

	var toShow []*Warning
	for _, w := range s.warnings {
		if !w.IsShowable(now) {
			continue
		}
		toShow = append(toShow, w)
	}

	sort.Sort(byLastAdded(toShow))
	return toShow, now
}

// WarningsSummary returns the number of warnings that are ready to be
// shown to the user, and the current timestamp (useful for ACKing the
// returned warnings).
func (s *State) WarningsSummary() (int, time.Time) {
	s.reading()
	now := time.Now().UTC()

	var n int
	for _, w := range s.warnings {
		if w.IsShowable(now) {
			n++
		}
	}

	return n, now
}

// UnshowAllWarnings clears the lastShown timestamp from all the
// warnings. For use in debugging.
func (s *State) UnshowAllWarnings() {
	s.writing()
	for _, w := range s.warnings {
		w.lastShown = time.Time{}
	}
}
