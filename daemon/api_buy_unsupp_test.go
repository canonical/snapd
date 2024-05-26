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

package daemon_test

import (
	"bytes"
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

var _ = check.Suite(&buySuite{})

type buySuite struct {
	apiBaseSuite

	user *auth.UserState
	err  error

	buyOptions *client.BuyOptions
	buyResult  *client.BuyResult
}

func (s *buySuite) Buy(options *client.BuyOptions, user *auth.UserState) (*client.BuyResult, error) {
	s.pokeStateLock()

	s.buyOptions = options
	s.user = user
	return s.buyResult, s.err
}

func (s *buySuite) ReadyToBuy(user *auth.UserState) error {
	s.pokeStateLock()

	s.user = user
	return s.err
}

func (s *buySuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.user = nil
	s.err = nil
	s.buyOptions = nil
	s.buyResult = nil

	s.daemonWithStore(c, s)

	s.expectAuthenticatedAccess()
}

const validBuyInput = `{
		  "snap-id": "the-snap-id-1234abcd",
		  "snap-name": "the snap name",
		  "price": 1.23,
		  "currency": "EUR"
		}`

var validBuyOptions = &client.BuyOptions{
	SnapID:   "the-snap-id-1234abcd",
	Price:    1.23,
	Currency: "EUR",
}

var buyTests = []struct {
	input                string
	result               *client.BuyResult
	err                  error
	expectedStatus       int
	expectedResult       interface{}
	expectedResponseType daemon.ResponseType
	expectedBuyOptions   *client.BuyOptions
}{
	{
		// Success
		input: validBuyInput,
		result: &client.BuyResult{
			State: "Complete",
		},
		expectedStatus: 200,
		expectedResult: &client.BuyResult{
			State: "Complete",
		},
		expectedResponseType: daemon.ResponseTypeSync,
		expectedBuyOptions:   validBuyOptions,
	},
	{
		// Fail with internal error
		input: `{
		  "snap-id": "the-snap-id-1234abcd",
		  "price": 1.23,
		  "currency": "EUR"
		}`,
		err:                  fmt.Errorf("internal error banana"),
		expectedStatus:       500,
		expectedResponseType: daemon.ResponseTypeError,
		expectedResult: &daemon.ErrorResult{
			Message: "internal error banana",
		},
		expectedBuyOptions: &client.BuyOptions{
			SnapID:   "the-snap-id-1234abcd",
			Price:    1.23,
			Currency: "EUR",
		},
	},
	{
		// Fail with unauthenticated error
		input:                validBuyInput,
		err:                  store.ErrUnauthenticated,
		expectedStatus:       400,
		expectedResponseType: daemon.ResponseTypeError,
		expectedResult: &daemon.ErrorResult{
			Message: "you need to log in first",
			Kind:    "login-required",
		},
		expectedBuyOptions: validBuyOptions,
	},
	{
		// Fail with TOS not accepted
		input:                validBuyInput,
		err:                  store.ErrTOSNotAccepted,
		expectedStatus:       400,
		expectedResponseType: daemon.ResponseTypeError,
		expectedResult: &daemon.ErrorResult{
			Message: "terms of service not accepted",
			Kind:    "terms-not-accepted",
		},
		expectedBuyOptions: validBuyOptions,
	},
	{
		// Fail with no payment methods
		input:                validBuyInput,
		err:                  store.ErrNoPaymentMethods,
		expectedStatus:       400,
		expectedResponseType: daemon.ResponseTypeError,
		expectedResult: &daemon.ErrorResult{
			Message: "no payment methods",
			Kind:    "no-payment-methods",
		},
		expectedBuyOptions: validBuyOptions,
	},
	{
		// Fail with payment declined
		input:                validBuyInput,
		err:                  store.ErrPaymentDeclined,
		expectedStatus:       400,
		expectedResponseType: daemon.ResponseTypeError,
		expectedResult: &daemon.ErrorResult{
			Message: "payment declined",
			Kind:    "payment-declined",
		},
		expectedBuyOptions: validBuyOptions,
	},
}

func (s *buySuite) TestBuySnap(c *check.C) {
	user := &auth.UserState{
		Username: "username",
		Email:    "email@test.com",
	}

	for _, test := range buyTests {
		s.buyResult = test.result
		s.err = test.err

		buf := bytes.NewBufferString(test.input)
		req := mylog.Check2(http.NewRequest("POST", "/v2/buy", buf))
		c.Assert(err, check.IsNil)

		rsp := s.jsonReq(c, req, user)

		c.Check(rsp.Status, check.Equals, test.expectedStatus)
		c.Check(rsp.Type, check.Equals, test.expectedResponseType)
		c.Assert(rsp.Result, check.FitsTypeOf, test.expectedResult)
		c.Check(rsp.Result, check.DeepEquals, test.expectedResult)

		c.Check(s.buyOptions, check.DeepEquals, test.expectedBuyOptions)
		c.Check(s.user, check.Equals, user)
	}
}

var readyToBuyTests = []struct {
	input    error
	status   int
	respType interface{}
	response interface{}
}{
	{
		// Success
		input:    nil,
		status:   200,
		respType: daemon.ResponseTypeSync,
		response: true,
	},
	{
		// Not accepted TOS
		input:    store.ErrTOSNotAccepted,
		status:   400,
		respType: daemon.ResponseTypeError,
		response: &daemon.ErrorResult{
			Message: "terms of service not accepted",
			Kind:    client.ErrorKindTermsNotAccepted,
		},
	},
	{
		// No payment methods
		input:    store.ErrNoPaymentMethods,
		status:   400,
		respType: daemon.ResponseTypeError,
		response: &daemon.ErrorResult{
			Message: "no payment methods",
			Kind:    client.ErrorKindNoPaymentMethods,
		},
	},
}

func (s *buySuite) TestReadyToBuy(c *check.C) {
	user := &auth.UserState{
		Username: "username",
		Email:    "email@test.com",
	}

	for _, test := range readyToBuyTests {
		s.err = test.input

		req := mylog.Check2(http.NewRequest("GET", "/v2/buy/ready", nil))
		c.Assert(err, check.IsNil)

		rsp := s.jsonReq(c, req, user)
		c.Check(rsp.Status, check.Equals, test.status)
		c.Check(rsp.Type, check.Equals, test.respType)
		c.Assert(rsp.Result, check.FitsTypeOf, test.response)
		c.Check(rsp.Result, check.DeepEquals, test.response)
	}
}
