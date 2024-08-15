// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/testutil"
)

var (
	GetUserID = getUserID
)

type PostPromptBody postPromptBody
type AddRuleContents addRuleContents
type RemoveRulesSelector removeRulesSelector
type PatchRuleContents patchRuleContents

// When the types have nested contents, must redefine with exported types.
type PostRulesRequestBody struct {
	Action         string               `json:"action"`
	AddRule        *AddRuleContents     `json:"rule,omitempty"`
	RemoveSelector *RemoveRulesSelector `json:"selector,omitempty"`
}

type PostRuleRequestBody struct {
	Action    string             `json:"action"`
	PatchRule *PatchRuleContents `json:"rule,omitempty"`
}

func MockInterfaceManager(manager interfaceManager) (restore func()) {
	restore = testutil.Backup(&getInterfaceManager)
	getInterfaceManager = func(c *Command) interfaceManager {
		return manager
	}
	return restore
}
