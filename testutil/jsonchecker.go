// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"encoding/json"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

type jsonEqualChecker struct {
	*check.CheckerInfo
}

// JsonEquals compares the obtained and expected values after having serialized
// them to JSON and deserialized to a generic interface{} type. This avoids
// trouble comparing types with unexported fields that otherwise would be
// problematic to set in external packages.
var JsonEquals check.Checker = &jsonEqualChecker{
	&check.CheckerInfo{Name: "JsonEqual", Params: []string{"obtained", "expected"}},
}

func (c *jsonEqualChecker) Check(params []interface{}, names []string) (result bool, error string) {
	toComparableMap := func(what interface{}) interface{} {
		b := mylog.Check2(json.Marshal(what))

		var back interface{}
		mylog.Check(json.Unmarshal(b, &back))

		return back
	}

	obtained := toComparableMap(params[0])
	ref := toComparableMap(params[1])

	return check.DeepEquals.Check([]interface{}{obtained, ref}, names)
}
