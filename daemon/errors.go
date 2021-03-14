// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2021 Canonical Ltd
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

package daemon

import (
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/client"
)

// XXX move more bits related to errors from response.go

// apiError reprents an error meant for returning to the client.
// It can serialize itself to our standard JSON response format.
type apiError struct {
	// Status is the error HTTP status code.
	Status  int
	Message string
	// Kind is the error kind. See client/errors.go
	Kind  client.ErrorKind
	Value errorValue
}

func (ae *apiError) Error() string {
	machine := "api"
	if ae.Kind != "" {
		machine = fmt.Sprintf("api: %s", ae.Kind)
	} else if ae.Status != 400 {
		machine = fmt.Sprintf("api %d", ae.Status)
	}
	return fmt.Sprintf("%s (%s)", ae.Message, machine)
}

func (ae *apiError) JSON() *respJSON {
	return &respJSON{
		Status: ae.Status,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: ae.Message,
			Kind:    ae.Kind,
			Value:   ae.Value,
		},
	}
}

func (ae *apiError) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ae.JSON().ServeHTTP(w, r)
}

// check it implements StructuredResponse
var _ StructuredResponse = (*apiError)(nil)
