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
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

func MockListenerRegister(f func() (*listener.Listener, error)) (restore func()) {
	return testutil.Mock(&listenerRegister, f)
}

func MockListenerRun(f func(l *listener.Listener) error) (restore func()) {
	return testutil.Mock(&listenerRun, f)
}

func MockListenerReqs(f func(l *listener.Listener) <-chan *listener.Request) (restore func()) {
	return testutil.Mock(&listenerReqs, f)
}

func MockListenerClose(f func(l *listener.Listener) error) (restore func()) {
	return testutil.Mock(&listenerClose, f)
}

type RequestResponse struct {
	Request           *listener.Request
	AllowedPermission any
}

func MockListener() (reqChan chan *listener.Request, replyChan chan RequestResponse, restore func()) {
	// Since the manager run loop is in a tracked goroutine, shouldn't block.
	reqChan = make(chan *listener.Request)
	// Replies would be sent synchronously to an async listener, but it's
	// mocked to be synchronous, so we need a non-zero buffer here.
	replyChan = make(chan RequestResponse, 5)

	closeChan := make(chan struct{})

	restoreRegister := MockListenerRegister(func() (*listener.Listener, error) {
		return &listener.Listener{}, nil
	})
	restoreRun := MockListenerRun(func(l *listener.Listener) error {
		<-closeChan
		return listener.ErrClosed
	})
	restoreReqs := MockListenerReqs(func(l *listener.Listener) <-chan *listener.Request {
		return reqChan
	})
	restoreClose := MockListenerClose(func(l *listener.Listener) error {
		select {
		case <-closeChan:
			return listener.ErrAlreadyClosed
		default:
			close(reqChan)
			close(replyChan)
			close(closeChan)
		}
		return nil
	})
	restoreReply := MockRequestReply(func(req *listener.Request, allowedPermission any) error {
		reqResp := RequestResponse{
			Request:           req,
			AllowedPermission: allowedPermission,
		}
		replyChan <- reqResp
		return nil
	})
	restore = func() {
		restoreReply()
		restoreClose()
		restoreReqs()
		restoreRun()
		restoreRegister()
	}
	return reqChan, replyChan, restore
}

func MockRequestReply(f func(req *listener.Request, allowedPermission any) error) (restore func()) {
	restoreRequestReply := testutil.Backup(&requestReply)
	requestReply = f
	restoreRequestpromptsSendReply := requestprompts.MockSendReply(f)
	return func() {
		restoreRequestpromptsSendReply()
		restoreRequestReply()
	}
}

func (m *InterfacesRequestsManager) PromptDB() *requestprompts.PromptDB {
	return m.prompts
}

func (m *InterfacesRequestsManager) RuleDB() *requestrules.RuleDB {
	return m.rules
}
