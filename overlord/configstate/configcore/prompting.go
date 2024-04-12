// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore

import (
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/restart"
)

// Trigger a security profile regeneration by restarting snapd if a change in
// the experimental apparmor-prompting flag causes a need to do so.
func doExperimentalApparmorPromptingDaemonRestart(c RunTransaction, opts *fsOnlyContext) error {
	st := c.State()

	snap, confName := features.AppArmorPrompting.ConfigOption()

	var prompting bool
	err := c.Get(snap, confName, &prompting)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	var prevPrompting bool
	err = c.GetPristine(snap, confName, &prevPrompting)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if prompting == prevPrompting {
		return nil
	}

	// XXX: what if apparmor-prompting flag value unchanged but support changed?
	// Prompting support (currently) depends only on AppArmor kernel and parser
	// features, which are checked only once at startup, so this cannot occur.

	if !features.AppArmorPrompting.IsSupported() {
		// prompting flag changed, but prompting unsupported, so no need to
		// regenerate security profiles via a restart.
		return nil
	}

	st.Lock()
	defer st.Unlock()

	restart.Request(st, restart.RestartDaemon, nil)

	return nil
}
