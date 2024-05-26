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

	"github.com/ddkwork/golibrary/mylog"
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
		isMany      bool
	}{
		{store.ErrSnapNotFound, daemon.SnapNotFound("foo", store.ErrSnapNotFound), false},
		{store.ErrNoUpdateAvailable, makeErrorRsp(client.ErrorKindSnapNoUpdateAvailable, store.ErrNoUpdateAvailable, ""), false},
		{store.ErrLocalSnap, makeErrorRsp(client.ErrorKindSnapLocal, store.ErrLocalSnap, ""), false},
		{aie, makeErrorRsp(client.ErrorKindSnapAlreadyInstalled, aie, "foo"), false},
		{nie, daemon.SnapNotInstalled("foo", nie), false},
		{ndme, makeErrorRsp(client.ErrorKindSnapNeedsDevMode, ndme, "foo"), false},
		{nc, makeErrorRsp(client.ErrorKindSnapNotClassic, nc, "foo"), false},
		{nce, makeErrorRsp(client.ErrorKindSnapNeedsClassic, nce, "foo"), false},
		{ncse, makeErrorRsp(client.ErrorKindSnapNeedsClassicSystem, ncse, "foo"), false},
		{cce, daemon.SnapChangeConflict(cce), false},
		{nettoute, makeErrorRsp(client.ErrorKindNetworkTimeout, nettoute, ""), false},
		{netoe, daemon.BadRequest("ERR: %v", netoe), false},
		{nettmpe, daemon.BadRequest("ERR: %v", nettmpe), false},
		{e, daemon.BadRequest("ERR: %v", e), false},

		// action error unwrapping:
		{sa1e, daemon.SnapNotFound("foo", store.ErrSnapNotFound), false},
		// simulates one snap not found (foo) and another one found (bar)
		// for context see: https://bugs.launchpad.net/snapd/+bug/2024858
		{sa1e, daemon.SnapNotFound("foo", store.ErrSnapNotFound), true},
		{saXe, daemon.SnapNotFound("foo", store.ErrSnapNotFound), false},
		// action errors, unwrapped:
		{sa2e, daemon.BadRequest(`ERR: cannot refresh: snap not found: "bar", "foo"`), true},
		{saOe, daemon.BadRequest("ERR: cannot refresh, install, or download: other error"), false},
	}

	for _, t := range tests {
		com := Commentf("%v", t.err)
		snaps := []string{"foo"}
		if t.isMany {
			snaps = append(snaps, "bar")
		}
		rspe := daemon.ErrToResponse(t.err, snaps, daemon.BadRequest, "%s: %v", "ERR")
		c.Check(rspe, DeepEquals, t.expectedRsp, com)
	}
}

func (errorsSuite) TestErrorResponderPrintfsWithArgs(c *C) {
	teapot := daemon.MakeErrorResponder(418)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below %d%%.", 1)
	req := mylog.Check2(http.NewRequest("GET", "", nil))

	rsp.ServeHTTP(rec, req)

	var v struct{ Result daemon.ErrorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), IsNil)

	c.Check(v.Result.Message, Equals, "system memory below 1%.")
}

func (errorsSuite) TestErrorResponderDoesNotPrintfAlways(c *C) {
	teapot := daemon.MakeErrorResponder(418)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below 1%.")
	req := mylog.Check2(http.NewRequest("GET", "", nil))

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

func (s *errorsSuite) TestAuthCancelled(c *C) {
	c.Check(daemon.AuthCancelled("auth cancelled"), DeepEquals, &daemon.APIError{
		Status:  403,
		Message: "auth cancelled",
		Kind:    client.ErrorKindAuthCancelled,
	})
}

func (s *errorsSuite) TestUnathorized(c *C) {
	c.Check(daemon.Unauthorized("denied"), DeepEquals, &daemon.APIError{
		Status:  401,
		Message: "denied",
		Kind:    client.ErrorKindLoginRequired,
	})
}

func (s *errorsSuite) TestForbidden(c *C) {
	c.Check(daemon.Forbidden("denied"), DeepEquals, &daemon.APIError{
		Status:  403,
		Message: "denied",
		Kind:    client.ErrorKindLoginRequired,
	})
}
