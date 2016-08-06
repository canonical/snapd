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
	"sync"
)

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	Error(err error) error

	// Get the data that this handler has associated with the given key.
	Get(key string) (map[string]interface{}, error)

	// Set the data that this handler has associated with the given key.
	Set(key string, data map[string]interface{}) error
}

// HandlerCollection is responsible for keeping track of a set of handlers
// accessed by unique ID.
type HandlerCollection struct {
	handlersMutex sync.RWMutex
	handlers      map[int]Handler
	lastID        int
}

// NewHandlerCollection returns a new HandlerCollection.
func NewHandlerCollection() *HandlerCollection {
	return &HandlerCollection{
		handlers: make(map[int]Handler),
	}
}

// HandlerCount returns the number of handlers in the collection.
func (c *HandlerCollection) HandlerCount() int {
	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()

	return len(c.handlers)
}

// GetHandlerData obtains the data that a specific handler has associated with
// the given key.
func (c *HandlerCollection) GetHandlerData(id int, key string) (map[string]interface{}, error) {
	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()

	if handler, ok := c.handlers[id]; ok {
		return handler.Get(key)
	}

	return nil, fmt.Errorf("no handler with ID %d", id)
}

// SetHandlerData sets the data that a specific handler has associated with the
// given key.
func (c *HandlerCollection) SetHandlerData(id int, key string, data map[string]interface{}) error {
	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()

	if handler, ok := c.handlers[id]; ok {
		return handler.Set(key, data)
	}

	return fmt.Errorf("no handler with ID %d", id)
}

// AddHandler adds a new handler to the collection. Returns the ID which can be
// used to refer to this handler in the future.
func (c *HandlerCollection) AddHandler(handler Handler) int {
	c.handlersMutex.Lock()
	defer c.handlersMutex.Unlock()

	c.lastID++
	c.handlers[c.lastID] = handler

	return c.lastID
}

// RemoveHandler removes the handler with the specified ID. If no such handler
// exists, this function does nothing.
func (c *HandlerCollection) RemoveHandler(id int) {
	c.handlersMutex.Lock()
	defer c.handlersMutex.Unlock()

	delete(c.handlers, id)
}
