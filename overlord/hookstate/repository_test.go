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

package hookstate

import (
	"fmt"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type repositorySuite struct{}

var _ = Suite(&repositorySuite{})

func (s *repositorySuite) TestAddHandlerGenerator(c *C) {
	repository := newRepository()

	var calledContext *Context
	mockHandlerGenerator := func(context *Context) Handler {
		calledContext = context
		return NewMockHandler()
	}

	// Verify that a handler generator can be added to the repository
	repository.addHandlerGenerator(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	state := state.New(nil)
	state.Lock()
	task := state.NewTask("test-task", "my test task")
	setup := hookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	context := &Context{task: task, setup: setup}
	state.Unlock()

	c.Assert(context, NotNil)

	// Verify that the handler can be generated
	handlers := repository.generateHandlers(context)
	c.Check(handlers, HasLen, 1)
	c.Check(calledContext, DeepEquals, context)

	// Add another handler
	repository.addHandlerGenerator(regexp.MustCompile(".*-hook"), mockHandlerGenerator)

	// Verify that two handlers are generated for the test-hook, now
	handlers = repository.generateHandlers(context)
	c.Check(handlers, HasLen, 2)
	c.Check(calledContext, DeepEquals, context)
}

type MockHandler struct {
	BeforeCalled bool
	BeforeError  bool

	DoneCalled bool
	DoneError  bool

	ErrorCalled bool
	ErrorError  bool
	Err         error

	GetCalled bool
	GetError  bool
	SetCalled bool
	SetError  bool
	Key       string
	Data      map[string]interface{}
}

func NewMockHandler() *MockHandler {
	return &MockHandler{
		BeforeCalled: false,
		BeforeError:  false,

		DoneCalled: false,
		DoneError:  false,

		ErrorCalled: false,
		ErrorError:  false,
		Err:         nil,

		GetCalled: false,
		GetError:  false,
		SetCalled: false,
		SetError:  false,
	}
}

func (h *MockHandler) Before() error {
	h.BeforeCalled = true
	if h.BeforeError {
		return fmt.Errorf("before failed at user request")
	}
	return nil
}

func (h *MockHandler) Done() error {
	h.DoneCalled = true
	if h.DoneError {
		return fmt.Errorf("done failed at user request")
	}
	return nil
}

func (h *MockHandler) Error(err error) error {
	h.Err = err
	h.ErrorCalled = true
	if h.ErrorError {
		return fmt.Errorf("error failed at user request")
	}
	return nil
}

func (h *MockHandler) Get(key string) (map[string]interface{}, error) {
	h.GetCalled = true
	h.Key = key
	if h.GetError {
		return nil, fmt.Errorf("get failed at user request")
	}
	return map[string]interface{}{"foo": "bar"}, nil
}

func (h *MockHandler) Set(key string, data map[string]interface{}) error {
	h.SetCalled = true
	h.Key = key
	h.Data = data
	if h.SetError {
		return fmt.Errorf("set failed at user request")
	}
	return nil
}
