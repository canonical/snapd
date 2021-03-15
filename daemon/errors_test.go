// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package daemon_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type errorsSuite struct{}

var _ = Suite(&errorsSuite{})

func (s *errorsSuite) TestJSON(c *C) {
	rspe := &daemon.APIError{
		Status:  400,
		Message: "req is wrong",
	}

	c.Check(rspe.JSON(), DeepEquals, &daemon.RespJSON{
		Status: 400,
		Type:   daemon.ResponseTypeError,
		Result: &daemon.ErrorResult{
			Message: "req is wrong",
		},
	})

	rspe = &daemon.APIError{
		Status:  404,
		Message: "snap not found",
		Kind:    client.ErrorKindSnapNotFound,
		Value: map[string]string{
			"snap-name": "foo",
		},
	}
	c.Check(rspe.JSON(), DeepEquals, &daemon.RespJSON{
		Status: 404,
		Type:   daemon.ResponseTypeError,
		Result: &daemon.ErrorResult{
			Message: "snap not found",
			Kind:    client.ErrorKindSnapNotFound,
			Value: map[string]string{
				"snap-name": "foo",
			},
		},
	})
}

func (s *errorsSuite) TestError(c *C) {
	rspe := &daemon.APIError{
		Status:  400,
		Message: "req is wrong",
	}

	c.Check(rspe.Error(), Equals, `req is wrong (api)`)

	rspe = &daemon.APIError{
		Status:  404,
		Message: "snap not found",
		Kind:    client.ErrorKindSnapNotFound,
		Value: map[string]string{
			"snap-name": "foo",
		},
	}

	c.Check(rspe.Error(), Equals, `snap not found (api: snap-not-found)`)

	rspe = &daemon.APIError{
		Status:  500,
		Message: "internal error",
	}
	c.Check(rspe.Error(), Equals, `internal error (api 500)`)
}

func (s *errorsSuite) TestThroughSyncResponse(c *C) {
	rspe := &daemon.APIError{
		Status:  400,
		Message: "req is wrong",
	}

	rsp := daemon.SyncResponse(rspe)
	c.Check(rsp, Equals, rspe)
}

type fakeNetError struct {
	message   string
	timeout   bool
	temporary bool
}

func (e fakeNetError) Error() string   { return e.message }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return e.temporary }

func (s *errorsSuite) TestErrToResponse(c *C) {
	aie := &snap.AlreadyInstalledError{Snap: "foo"}
	nie := &snap.NotInstalledError{Snap: "foo"}
	cce := &snapstate.ChangeConflictError{Snap: "foo"}
	ndme := &snapstate.SnapNeedsDevModeError{Snap: "foo"}
	nc := &snapstate.SnapNotClassicError{Snap: "foo"}
	nce := &snapstate.SnapNeedsClassicError{Snap: "foo"}
	ncse := &snapstate.SnapNeedsClassicSystemError{Snap: "foo"}
	netoe := fakeNetError{message: "other"}
	nettoute := fakeNetError{message: "timeout", timeout: true}
	nettmpe := fakeNetError{message: "temp", temporary: true}

	e := errors.New("other error")

	sa1e := &store.SnapActionError{Refresh: map[string]error{"foo": store.ErrSnapNotFound}}
	sa2e := &store.SnapActionError{Refresh: map[string]error{
		"foo": store.ErrSnapNotFound,
		"bar": store.ErrSnapNotFound,
	}}
	saOe := &store.SnapActionError{Other: []error{e}}
	// this one can't happen (but fun to test):
	saXe := &store.SnapActionError{Refresh: map[string]error{"foo": sa1e}}

	makeErrorRsp := func(kind client.ErrorKind, err error, value interface{}) *daemon.APIError {
		return &daemon.APIError{
			Status:  400,
			Message: err.Error(),
			Kind:    kind,
			Value:   value,
		}
	}

	tests := []struct {
		err         error
		expectedRsp daemon.Response
	}{
		{store.ErrSnapNotFound, daemon.SnapNotFound("foo", store.ErrSnapNotFound)},
		{store.ErrNoUpdateAvailable, makeErrorRsp(client.ErrorKindSnapNoUpdateAvailable, store.ErrNoUpdateAvailable, "")},
		{store.ErrLocalSnap, makeErrorRsp(client.ErrorKindSnapLocal, store.ErrLocalSnap, "")},
		{aie, makeErrorRsp(client.ErrorKindSnapAlreadyInstalled, aie, "foo")},
		{nie, makeErrorRsp(client.ErrorKindSnapNotInstalled, nie, "foo")},
		{ndme, makeErrorRsp(client.ErrorKindSnapNeedsDevMode, ndme, "foo")},
		{nc, makeErrorRsp(client.ErrorKindSnapNotClassic, nc, "foo")},
		{nce, makeErrorRsp(client.ErrorKindSnapNeedsClassic, nce, "foo")},
		{ncse, makeErrorRsp(client.ErrorKindSnapNeedsClassicSystem, ncse, "foo")},
		{cce, daemon.SnapChangeConflict(cce)},
		{nettoute, makeErrorRsp(client.ErrorKindNetworkTimeout, nettoute, "")},
		{netoe, daemon.BadRequest("ERR: %v", netoe)},
		{nettmpe, daemon.BadRequest("ERR: %v", nettmpe)},
		{e, daemon.BadRequest("ERR: %v", e)},

		// action error unwrapping:
		{sa1e, daemon.SnapNotFound("foo", store.ErrSnapNotFound)},
		{saXe, daemon.SnapNotFound("foo", store.ErrSnapNotFound)},
		// action errors, unwrapped:
		{sa2e, daemon.BadRequest(`ERR: cannot refresh: snap not found: "bar", "foo"`)},
		{saOe, daemon.BadRequest("ERR: cannot refresh, install, or download: other error")},
	}

	for _, t := range tests {
		com := Commentf("%v", t.err)
		rspe := daemon.ErrToResponse(t.err, []string{"foo"}, daemon.BadRequest, "%s: %v", "ERR")
		c.Check(rspe, DeepEquals, t.expectedRsp, com)
	}
}

func (errorsSuite) TestErrorResponderPrintfsWithArgs(c *C) {
	teapot := daemon.MakeErrorResponder(418)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below %d%%.", 1)
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, IsNil)
	rsp.ServeHTTP(rec, req)

	var v struct{ Result daemon.ErrorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), IsNil)

	c.Check(v.Result.Message, Equals, "system memory below 1%.")
}

func (errorsSuite) TestErrorResponderDoesNotPrintfAlways(c *C) {
	teapot := daemon.MakeErrorResponder(418)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below 1%.")
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, IsNil)
	rsp.ServeHTTP(rec, req)

	var v struct{ Result daemon.ErrorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), IsNil)

	c.Check(v.Result.Message, Equals, "system memory below 1%.")
}

func (s *errorsSuite) TestErrToResponseInsufficentSpace(c *C) {
	err := &snapstate.InsufficientSpaceError{
		Snaps:      []string{"foo", "bar"},
		ChangeKind: "some-change",
		Path:       "/path",
		Message:    "specific error msg",
	}
	rspe := daemon.ErrToResponse(err, nil, daemon.BadRequest, "%s: %v", "ERR")
	c.Check(rspe, DeepEquals, &daemon.APIError{
		Status:  507,
		Message: "specific error msg",
		Kind:    client.ErrorKindInsufficientDiskSpace,
		Value: map[string]interface{}{
			"snap-names":  []string{"foo", "bar"},
			"change-kind": "some-change",
		},
	})
}
