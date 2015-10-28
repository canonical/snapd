// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ubuntu-core/snappy/logger"
)

// ResponseType is the response type
type ResponseType string

// “there are three standard return types: Standard return value,
// Background operation, Error”, each returning a JSON object with the
// following “type” field:
const (
	ResponseTypeSync  ResponseType = "sync"
	ResponseTypeAsync ResponseType = "async"
	ResponseTypeError ResponseType = "error"
)

// Response knows how to serve itself, and how to find itself
type Response interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Self(*Command, *http.Request) Response // has the same arity as ResponseFunc for convenience
}

type resp struct {
	Type   ResponseType `json:"type"`
	Status int          `json:"status_code"`
	Result interface{}  `json:"result"`
}

func (r *resp) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":        r.Type,
		"status":      http.StatusText(r.Status),
		"status_code": r.Status,
		"result":      &r.Result,
	})
}

func (r *resp) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	bs, err := r.MarshalJSON()
	if err != nil {
		logger.Noticef("unable to marshal %#v to JSON: %v", *r, err)
		bs = nil
		status = http.StatusInternalServerError
	}

	hdr := w.Header()
	if r.Type == ResponseTypeAsync {
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

func (r *resp) Self(*Command, *http.Request) Response {
	return r
}

type errorResult struct {
	Str string `json:"str,omitempty"`
	Msg string `json:"msg,omitempty"`
	Obj error  `json:"obj,omitempty"`
}

func (r *resp) SetError(err error, format string, v ...interface{}) Response {
	m := errorResult{}
	newr := &resp{
		Type:   ResponseTypeError,
		Result: &m,
		Status: r.Status,
	}

	if format != "" {
		logger.Noticef(format, v...)
		m.Msg = fmt.Sprintf(format, v...)
	}

	if err != nil {
		m.Obj = err
		m.Str = err.Error()
	}

	return newr
}

// SyncResponse builds a "sync" response from the given result.
func SyncResponse(result interface{}) Response {
	if err, ok := result.(error); ok {
		return InternalError(err, "")
	}

	if rsp, ok := result.(Response); ok {
		return rsp
	}

	return &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: result,
	}
}

// AsyncResponse builds an "async" response from the given *Task
func AsyncResponse(result map[string]interface{}) Response {
	return &resp{
		Type:   ResponseTypeAsync,
		Status: http.StatusAccepted,
		Result: result,
	}
}

// ErrorResponse builds an "error" response from the given error status.
func ErrorResponse(status int) ErrorResponseFunc {
	r := &resp{
		Type:   ResponseTypeError,
		Status: status,
	}

	return r.SetError
}

// A FileResponse 's ServeHTTP method serves the file
type FileResponse string

// Self from the Response interface
func (f FileResponse) Self(*Command, *http.Request) Response { return f }

// ServeHTTP from the Response interface
func (f FileResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, string(f))
}

// ErrorResponseFunc is a callable error Response.
// So you can return e.g. InternalError, or InternalError(err, "something broke"), etc.
type ErrorResponseFunc func(error, string, ...interface{}) Response

func (f ErrorResponseFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(nil, "").ServeHTTP(w, r)
}

// Self returns (a copy of) this same response; mostly for convenience.
func (f ErrorResponseFunc) Self(*Command, *http.Request) Response {
	return f(nil, "")
}

// standard error responses
var (
	NotFound       = ErrorResponse(http.StatusNotFound)
	BadRequest     = ErrorResponse(http.StatusBadRequest)
	BadMethod      = ErrorResponse(http.StatusMethodNotAllowed)
	InternalError  = ErrorResponse(http.StatusInternalServerError)
	NotImplemented = ErrorResponse(http.StatusNotImplemented)
)
