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

package registrystate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
)

func init() {
	hookstate.ViewChangedHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &viewChangedHandler{ctx: context}
	}
}

type viewChangedHandler struct {
	hookstate.SnapHookHandler
	ctx *hookstate.Context
}

func (h *viewChangedHandler) Precondition() (bool, error) {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	// check that the plug is still connected
	plugName, _, ok := strings.Cut(h.ctx.HookName(), "-view-changed")
	if !ok || plugName == "" {
		return false, fmt.Errorf("cannot run registry hook handler for unknown hook: %s", h.ctx.HookName())
	}

	repo := ifacerepo.Get(h.ctx.State())
	conns, err := repo.Connected(h.ctx.InstanceName(), plugName)
	if err != nil {
		var verr *interfaces.NoPlugOrSlotError
		if errors.As(err, &verr) {
			return false, nil
		}

		return false, fmt.Errorf("cannot determine precondition for hook %s: %w", h.ctx.HookName(), err)
	}

	return len(conns) > 0, nil
}
