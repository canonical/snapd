// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package testutil

import (
	"errors"
	"fmt"

	"gopkg.in/check.v1"
)

// ErrorIs calls errors.Is with the provided arguments.
var ErrorIs = &errorIsChecker{
	&check.CheckerInfo{Name: "ErrorIs", Params: []string{"error", "target"}},
}

type errorIsChecker struct {
	*check.CheckerInfo
}

func (*errorIsChecker) Check(params []interface{}, names []string) (result bool, errMsg string) {
	if params[0] == nil {
		return params[1] == nil, ""
	}

	err, ok := params[0].(error)
	if !ok {
		return false, fmt.Sprintf("first argument must be an error")
	}

	target, ok := params[1].(error)
	if !ok {
		return false, fmt.Sprintf("second argument must be an error")
	}

	return errors.Is(err, target), ""
}
