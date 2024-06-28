// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"time"
)

// MockCheckpointRetryDelay changes unlockCheckpointRetryInterval and unlockCheckpointRetryMaxTime.
func MockCheckpointRetryDelay(retryInterval, retryMaxTime time.Duration) (restore func()) {
	oldInterval := unlockCheckpointRetryInterval
	oldMaxTime := unlockCheckpointRetryMaxTime
	unlockCheckpointRetryInterval = retryInterval
	unlockCheckpointRetryMaxTime = retryMaxTime
	return func() {
		unlockCheckpointRetryInterval = oldInterval
		unlockCheckpointRetryMaxTime = oldMaxTime
	}
}

func MockChangeTimes(chg *Change, spawnTime, readyTime time.Time) {
	chg.spawnTime = spawnTime
	chg.readyTime = readyTime
}

func MockTaskTimes(t *Task, spawnTime, readyTime time.Time) {
	t.spawnTime = spawnTime
	t.readyTime = readyTime
}

func (w Warning) LastAdded() time.Time {
	return w.lastAdded
}

func (t *Task) AccumulateDoingTime(duration time.Duration) {
	t.accumulateDoingTime(duration)
}

func (t *Task) AccumulateUndoingTime(duration time.Duration) {
	t.accumulateUndoingTime(duration)
}

var (
	DefaultWarningExpireAfter = defaultWarningExpireAfter
	DefaultWarningRepeatAfter = defaultWarningRepeatAfter

	ErrNoWarningMessage     = errNoWarningMessage
	ErrBadWarningMessage    = errBadWarningMessage
	ErrNoWarningFirstAdded  = errNoWarningFirstAdded
	ErrNoWarningExpireAfter = errNoWarningExpireAfter
)

// NumNotices returns the total bumber of notices, including expired ones that
// haven't yet been pruned.
func (s *State) NumNotices() int {
	return len(s.notices)
}
