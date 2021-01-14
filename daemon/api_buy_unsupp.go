/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"net/http"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

// TODO: this is unsupported and when supported will have a new implementation,
// make these simply return errors and remove all the related code from
// the store package except maybe errors

var (
	buyCmd = &Command{
		Path: "/v2/buy",
		POST: postBuy,
	}

	readyToBuyCmd = &Command{
		Path: "/v2/buy/ready",
		GET:  readyToBuy,
	}
)

func postBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	var opts client.BuyOptions

	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&opts)
	if err != nil {
		return BadRequest("cannot decode buy options from request body: %v", err)
	}

	s := getStore(c)

	buyResult, err := s.Buy(&opts, user)

	if resp := convertBuyError(err); resp != nil {
		return resp
	}

	return SyncResponse(buyResult, nil)
}

func readyToBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	s := getStore(c)

	if resp := convertBuyError(s.ReadyToBuy(user)); resp != nil {
		return resp
	}

	return SyncResponse(true, nil)
}

func convertBuyError(err error) Response {
	switch err {
	case nil:
		return nil
	case store.ErrInvalidCredentials:
		return Unauthorized(err.Error())
	case store.ErrUnauthenticated:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindLoginRequired,
			},
			Status: 400,
		}, nil)
	case store.ErrTOSNotAccepted:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindTermsNotAccepted,
			},
			Status: 400,
		}, nil)
	case store.ErrNoPaymentMethods:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindNoPaymentMethods,
			},
			Status: 400,
		}, nil)
	case store.ErrPaymentDeclined:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindPaymentDeclined,
			},
			Status: 400,
		}, nil)
	default:
		return InternalError("%v", err)
	}
}
