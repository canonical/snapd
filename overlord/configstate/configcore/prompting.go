// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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
	"fmt"
	"time"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func doExperimentalApparmorPromptProfileRegeneration(c RunTransaction, opts *fsOnlyContext) error {
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

	st.Lock()
	regenerateProfilesChg := st.NewChange("regenerate-all-security-profiles",
		i18n.G("Regenerate all profiles due to change in prompting"))
	t := st.NewTask("regenerate-all-security-profiles", i18n.G("Regenerate all profiles due to change in prompting"))
	usePromptPrefix := prompting && features.AppArmorPrompting.IsSupported()
	t.Set("use-prompt-prefix", usePromptPrefix)
	regenerateProfilesChg.AddTask(t)
	st.Unlock()
	st.EnsureBefore(0)

	select {
	case <-regenerateProfilesChg.Ready():
		st.Lock()
		defer st.Unlock()
		return regenerateProfilesChg.Err()
	case <-time.After(10 * time.Minute):
		// profile generate may take some time
		return fmt.Errorf("%s is taking too long", regenerateProfilesChg.Kind())
	}
}
