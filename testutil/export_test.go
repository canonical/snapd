// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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
	"gopkg.in/check.v1"
)

func UnexpectedIntChecker(relation string) *intChecker {
	return &intChecker{CheckerInfo: &check.CheckerInfo{Name: "unexpected", Params: []string{"a", "b"}}, rel: relation}
}

func MockShellcheckPath(p string) (restore func()) {
	return Mock(&shellcheckPath, p)
}

func MockRuntimeARCH(new string) (restore func()) {
	return Mock(&runtimeGOARCH, new)
}
