// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"fmt"
	"reflect"

	"gopkg.in/check.v1"
)

// IsInterfaceNil checks that the value is a nil interface value (<nil>).
var IsInterfaceNil = &interfaceNilChecker{
	&check.CheckerInfo{Name: "IsInterfaceNil", Params: []string{"value"}},
}

type interfaceNilChecker struct {
	*check.CheckerInfo
}

func (*interfaceNilChecker) Check(params []any, names []string) (result bool, errMsg string) {
	if reflect.ValueOf(params[0]).IsValid() {
		return false, fmt.Sprintf("expected <nil> but got %T type", params[0])
	}

	return true, ""
}
