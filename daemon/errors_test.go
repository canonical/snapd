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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
)

type errorsSuite struct{}

var _ = Suite(&errorsSuite{})

func (s *errorsSuite) TestJSON(c *C) {
	ae := &daemon.APIError{
		Status:  400,
		Message: "req is wrong",
	}

	c.Check(ae.JSON(), DeepEquals, &daemon.RespJSON{
		Status: 400,
		Type:   daemon.ResponseTypeError,
		Result: &daemon.ErrorResult{
			Message: "req is wrong",
		},
	})

	ae = &daemon.APIError{
		Status:  404,
		Message: "snap not found",
		Kind:    client.ErrorKindSnapNotFound,
		Value: map[string]string{
			"snap-name": "foo",
		},
	}
	c.Check(ae.JSON(), DeepEquals, &daemon.RespJSON{
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
	ae := &daemon.APIError{
		Status:  400,
		Message: "req is wrong",
	}

	c.Check(ae.Error(), Equals, `req is wrong (api)`)

	ae = &daemon.APIError{
		Status:  404,
		Message: "snap not found",
		Kind:    client.ErrorKindSnapNotFound,
		Value: map[string]string{
			"snap-name": "foo",
		},
	}

	c.Check(ae.Error(), Equals, `snap not found (api: snap-not-found)`)

	ae = &daemon.APIError{
		Status:  500,
		Message: "internal error",
	}
	c.Check(ae.Error(), Equals, `internal error (api 500)`)
}
