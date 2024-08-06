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
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

func MockPromptingEnabled(f func() bool) (restore func()) {
	restore = testutil.Backup(&PromptingEnabled)
	PromptingEnabled = f
	return restore
}

func MockNotifySupportAvailable(f func() bool) (restore func()) {
	restore = testutil.Backup(&notifySupportAvailable)
	notifySupportAvailable = f
	return restore
}

func MockListenerRegister(f func() (*listener.Listener, error)) (restore func()) {
	restore = testutil.Backup(&listenerRegister)
	listenerRegister = f
	return restore
}

func MockListenerRun(f func(l *listener.Listener) error) (restore func()) {
	restore = testutil.Backup(&listenerRun)
	listenerRun = f
	return restore
}

func MockListenerReqs(f func(l *listener.Listener) <-chan *listener.Request) (restore func()) {
	restore = testutil.Backup(&listenerRun)
	listenerReqs = f
	return restore
}

func MockListenerClose(f func(l *listener.Listener) error) (restore func()) {
	restore = testutil.Backup(&listenerClose)
	listenerClose = f
	return restore
}

func MockListener() (restore func()) {
	restoreSupport := MockNotifySupportAvailable(func() bool {
		return true
	})
	closeChan := make(chan *listener.Request)
	restoreRegister := MockListenerRegister(func() (*listener.Listener, error) {
		return &listener.Listener{}, nil
	})
	restoreRun := MockListenerRun(func(l *listener.Listener) error {
		<-closeChan
		return listener.ErrClosed
	})
	restoreReqs := MockListenerReqs(func(l *listener.Listener) <-chan *listener.Request {
		return closeChan
	})
	restoreClose := MockListenerClose(func(l *listener.Listener) error {
		select {
		case <-closeChan:
			return listener.ErrAlreadyClosed
		default:
			close(closeChan)
		}
		return nil
	})
	return func() {
		restoreSupport()
		restoreRegister()
		restoreRun()
		restoreReqs()
		restoreClose()
	}
}
