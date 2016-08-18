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
	"crypto/rand"
	"encoding/base64"
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
}

// handlerCollection is responsible for keeping track of a set of handlers
// accessed by unique ID.
type handlerCollection struct {
	handlersMutex sync.RWMutex
	handlers      map[string]Handler
}

// newhandlerCollection returns a new handlerCollection.
func newHandlerCollection() *handlerCollection {
	return &handlerCollection{
		handlers: make(map[string]Handler),
	}
}

// handlerCount returns the number of handlers in the collection.
func (c *handlerCollection) handlerCount() int {
	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()

	return len(c.handlers)
}

func (c *handlerCollection) getHandler(context string) (Handler, error) {
	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()

	handler, ok := c.handlers[context]
	if !ok {
		return nil, fmt.Errorf("no handler for context %q", context)
	}

	return handler, nil
}

// addHandler adds a new handler to the collection. Returns the ID which can be
// used to refer to this handler in the future.
func (c *handlerCollection) addHandler(handler Handler) (string, error) {
	c.handlersMutex.Lock()
	defer c.handlersMutex.Unlock()

	// Generate a secure, random ID for this handler
	idBytes := make([]byte, 32)
	_, err := rand.Read(idBytes)
	if err != nil {
		return "", fmt.Errorf("cannot generate handler ID: %s", err)
	}

	id := base64.URLEncoding.EncodeToString(idBytes)
	c.handlers[id] = handler

	return id, nil
}

// removeHandler removes the handler with the specified ID. If no such handler
// exists, this function does nothing.
func (c *handlerCollection) removeHandler(id string) {
	c.handlersMutex.Lock()
	defer c.handlersMutex.Unlock()

	delete(c.handlers, id)
}
