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

package apparmorprompting

import (
	"time"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

type ListenerBackend = listenerBackend

func MockListenerRegister(f func() (listenerBackend, error)) (restore func()) {
	return testutil.Mock(&listenerRegister, f)
}

type fakeListener struct {
	readyChan chan struct{}
	reqsChan  chan *prompting.Request
	closeChan chan struct{}
}

func (l *fakeListener) Close() error {
	select {
	case <-l.closeChan:
		return listener.ErrAlreadyClosed
	default:
		close(l.reqsChan)
		close(l.closeChan)
	}
	select {
	case <-l.readyChan:
		// already closed
	default:
		close(l.readyChan)
	}
	return nil
}

func (l *fakeListener) Run() error {
	<-l.closeChan
	// In production, listener.Run() does not return on error, and when
	// the listener is closed, it returns nil. So it should always return
	// nil in practice.
	return nil
}

func (l *fakeListener) Ready() <-chan struct{} {
	return l.readyChan
}

func (l *fakeListener) Reqs() <-chan *prompting.Request {
	return l.reqsChan
}

func MockListener() (readyChan chan struct{}, reqChan chan *prompting.Request, restore func()) {
	// The readyChan should be closed once all pending previously-sent requests
	// have been re-sent.
	readyChan = make(chan struct{})
	// Since the manager run loop is in a tracked goroutine, shouldn't block.
	reqChan = make(chan *prompting.Request)

	closeChan := make(chan struct{})

	restore = MockListenerRegister(func() (listenerBackend, error) {
		return &fakeListener{
			readyChan: readyChan,
			reqsChan:  reqChan,
			closeChan: closeChan,
		}, nil
	})
	return readyChan, reqChan, restore
}

// Export the manager-level ready channel so it can be used in tests.
func (m *InterfacesRequestsManager) Ready() <-chan struct{} {
	return m.ready
}

func MockPromptsHandleReadying(f func(pdb *requestprompts.PromptDB) error) (restore func()) {
	return testutil.Mock(&promptsHandleReadying, f)
}

func (m *InterfacesRequestsManager) PromptDB() *requestprompts.PromptDB {
	return m.prompts
}

func (m *InterfacesRequestsManager) RuleDB() *requestrules.RuleDB {
	return m.rules
}

var (
	NewNoticeBackends   = newNoticeBackends
	RegisterWithManager = (*noticeBackends).registerWithManager
)

func (nb *noticeBackends) PromptBackend() *noticeTypeBackend {
	return nb.promptBackend
}

func (nb *noticeBackends) RuleBackend() *noticeTypeBackend {
	return nb.ruleBackend
}

func (ntb *noticeTypeBackend) AddNotice(userID uint32, id prompting.IDType, data map[string]string) error {
	return ntb.addNotice(userID, id, data)
}

type NtbFilter = ntbFilter

func (ntb *noticeTypeBackend) SimplifyFilter(filter *state.NoticeFilter) (simplified ntbFilter, matchPossible bool) {
	return ntb.simplifyFilter(filter)
}

func (ntb *noticeTypeBackend) Save() error {
	return ntb.save()
}

// WaitUntilMutexHeld polls the backend's RWMutex until it sees that it is
// held, or until the given timeout elapses. Returns true if the mutex was
// seen to be held, or false if it timed out.
func WaitUntilMutexHeld(ntb *noticeTypeBackend, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	for {
		// Poll the lock 100 times before checking the timeout
		for i := 0; i < 100; i++ {
			if ntb.rwmu.TryLock() {
				ntb.rwmu.Unlock()
			} else {
				// Lock failed, so we know it's held elsewhere
				return true
			}
			// Sleep so we give other goroutines the chance to be scheduled
			time.Sleep(time.Nanosecond)
		}
		select {
		case <-timer.C:
			return false
		default:
		}
	}
}
