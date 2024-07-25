// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

package agent

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/logger"
)

// TODO: clean up unused code further after we have progressed enough
// to have a clear sense of what is untested and uneeded here

// ResponseType is the response type
type ResponseType string

// "there are three standard return types: Standard return value,
// Background operation, Error", each returning a JSON object with the
// following "type" field:
const (
	ResponseTypeSync  ResponseType = "sync"
	ResponseTypeAsync ResponseType = "async"
	ResponseTypeError ResponseType = "error"
)

// Response knows how to serve itself, and how to find itself
type Response interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type resp struct {
	Status int // HTTP status code
	Type   ResponseType
	Result interface{}
}

type respJSON struct {
	Type   ResponseType `json:"type"`
	Result interface{}  `json:"result"`
}

func (r *resp) MarshalJSON() ([]byte, error) {
	return json.Marshal(respJSON{
		Type:   r.Type,
		Result: r.Result,
	})
}

func (r *resp) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	bs, err := r.MarshalJSON()
	if err != nil {
		logger.Noticef("cannot marshal %#v to JSON: %v", *r, err)
		bs = nil
		status = 500
	}

	hdr := w.Header()
	if r.Status == 202 || r.Status == 201 {
		if m, ok := r.Result.(map[string]interface{}); ok {
			if location, ok := m["resource"]; ok {
				if location, ok := location.(string); ok && location != "" {
					hdr.Set("Location", location)
				}
			}
		}
	}

	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(bs)
}

type errorKind string

const (
	errorKindLoginRequired  = errorKind("login-required")
	errorKindServiceControl = errorKind("service-control")
	errorKindServiceStatus  = errorKind("service-status")
	errorKindAppControl     = errorKind("app-control")
)

type errorValue interface{}

type errorResult struct {
	Message string     `json:"message"` // mandatory in error responses
	Kind    errorKind  `json:"kind,omitempty"`
	Value   errorValue `json:"value,omitempty"`
}

// SyncResponse builds a "sync" response from the given result.
func SyncResponse(result interface{}) Response {
	if err, ok := result.(error); ok {
		return InternalError("internal error: %v", err)
	}

	if rsp, ok := result.(Response); ok {
		return rsp
	}

	return &resp{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: result,
	}
}

// AsyncResponse builds an "async" response from the given *Task
func AsyncResponse(result map[string]interface{}) Response {
	return &resp{
		Type:   ResponseTypeAsync,
		Status: 202,
		Result: result,
	}
}

// makeErrorResponder builds an errorResponder from the given error status.
func makeErrorResponder(status int) errorResponder {
	return func(format string, v ...interface{}) Response {
		res := &errorResult{}
		if len(v) == 0 {
			res.Message = format
		} else {
			res.Message = fmt.Sprintf(format, v...)
		}
		if status == 401 {
			res.Kind = errorKindLoginRequired
		}
		return &resp{
			Type:   ResponseTypeError,
			Result: res,
			Status: status,
		}
	}
}

// errorResponder is a callable that produces an error Response.
// e.g., InternalError("something broke: %v", err), etc.
type errorResponder func(string, ...interface{}) Response

// standard error responses
var (
	Unauthorized     = makeErrorResponder(401)
	NotFound         = makeErrorResponder(404)
	BadRequest       = makeErrorResponder(400)
	MethodNotAllowed = makeErrorResponder(405)
	InternalError    = makeErrorResponder(500)
	NotImplemented   = makeErrorResponder(501)
	Forbidden        = makeErrorResponder(403)
	Conflict         = makeErrorResponder(409)
)
