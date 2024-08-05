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

package requestprompts

import (
	"time"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

const MaxOutstandingPromptsPerUser = maxOutstandingPromptsPerUser

func MockSendReply(f func(listenerReq *listener.Request, reply *listener.Response) error) (restore func()) {
	restore = testutil.Backup(&sendReply)
	sendReply = f
	return restore
}

func (pc *promptConstraints) Path() string {
	return pc.path
}

func (pc *promptConstraints) RemainingPermissions() []string {
	return pc.remainingPermissions
}

func NewPrompt(id prompting.IDType, timestamp time.Time, snap string, iface string, path string, remainingPermissions []string, availablePermissions []string, originalPermissions []string) *Prompt {
	constraints := &promptConstraints{
		path:                 path,
		remainingPermissions: remainingPermissions,
		availablePermissions: availablePermissions,
		originalPermissions:  originalPermissions,
	}
	return &Prompt{
		ID:           id,
		Timestamp:    timestamp,
		Snap:         snap,
		Interface:    iface,
		Constraints:  constraints,
		listenerReqs: nil,
	}
}

func (pdb *PromptDB) PerUser() map[uint32]*userPromptDB {
	return pdb.perUser
}

func (pdb *PromptDB) NextID() (prompting.IDType, error) {
	return pdb.nextID()
}
