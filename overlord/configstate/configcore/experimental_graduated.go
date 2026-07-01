// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
)

func isGraduatedExperimentalChange(k string) bool {
	feature, ok := strings.CutPrefix(k, "core.experimental.")
	return ok && features.IsGraduated(feature)
}

func dropGraduatedExperimentalChange(cfg RunTransaction, k string) error {
	feature, ok := strings.CutPrefix(k, "core.experimental.")
	if !ok {
		return errors.New("internal error: change is not an experimental feature")
	}

	if err := cfg.Set("core", "experimental."+feature, nil); err != nil {
		return err
	}

	msg := fmt.Sprintf("feature %s is no longer experimental and is always enabled", feature)
	logger.Noticef("%s", msg)

	st := cfg.State()
	st.Lock()
	defer st.Unlock()

	if task := cfg.Task(); task != nil {
		task.Logf("%s", msg)
	}
	st.Warnf("%s", msg)

	return nil
}
