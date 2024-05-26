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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

// TODO: this is unsupported and when supported will have a new implementation,
// make these simply return errors and remove all the related code from
// the store package except maybe errors

var (
	buyCmd = &Command{
		Path:        "/v2/buy",
		POST:        postBuy,
		WriteAccess: authenticatedAccess{},
	}

	readyToBuyCmd = &Command{
		Path:       "/v2/buy/ready",
		GET:        readyToBuy,
		ReadAccess: authenticatedAccess{},
	}
)

func postBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	var opts client.BuyOptions

	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&opts))

	s := storeFrom(c.d)

	buyResult := mylog.Check2(s.Buy(&opts, user))

	if resp := convertBuyError(err); resp != nil {
		return resp
	}

	return SyncResponse(buyResult)
}

func readyToBuy(c *Command, r *http.Request, user *auth.UserState) Response {
	s := storeFrom(c.d)

	if resp := convertBuyError(s.ReadyToBuy(user)); resp != nil {
		return resp
	}

	return SyncResponse(true)
}

func convertBuyError(err error) Response {
	var kind client.ErrorKind
	switch err {
	case nil:
		return nil
	case store.ErrInvalidCredentials:
		return Unauthorized(err.Error())
	case store.ErrUnauthenticated:
		kind = client.ErrorKindLoginRequired
	case store.ErrTOSNotAccepted:
		kind = client.ErrorKindTermsNotAccepted
	case store.ErrNoPaymentMethods:
		kind = client.ErrorKindNoPaymentMethods
	case store.ErrPaymentDeclined:
		kind = client.ErrorKindPaymentDeclined
	default:
		return InternalError("%v", err)
	}
	return &apiError{
		Status:  400,
		Message: err.Error(),
		Kind:    kind,
	}
}
