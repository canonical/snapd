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
	"regexp"
	"sync"
)

// repository stores all registered handler generators, and generates registered
// handlers.
type Repository struct {
	mutex      sync.RWMutex
	generators []patternGeneratorPair
}

// patternGeneratorPair contains a hook handler generator and its corresponding
// regex pattern for what hook name should cause it to be called.
type patternGeneratorPair struct {
	pattern   *regexp.Regexp
	generator HandlerGenerator
}

// NewRepository creates an empty handler generator repository.
func NewRepository() *Repository {
	return &Repository{}
}

// AddHandler adds the provided handler generator to the repository.
func (r *Repository) AddHandlerGenerator(pattern *regexp.Regexp, generator HandlerGenerator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.generators = append(r.generators, patternGeneratorPair{
		pattern:   pattern,
		generator: generator,
	})
}

// GenerateHandlers calls the handler generators whose patterns match the
// hook name contained within the provided context, and returns the resulting
// handlers.
func (r *Repository) GenerateHandlers(context *Context) []Handler {
	hookName := context.HookName()
	var handlers []Handler

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, pair := range r.generators {
		if pair.pattern.MatchString(hookName) {
			handlers = append(handlers, pair.generator(context))
		}
	}

	return handlers
}

// HandlerGenerator is the function signature required to register for hooks.
type HandlerGenerator func(*Context) Handler

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	Error(err error) error
}
