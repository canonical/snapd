// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers

import (
	"bytes"
	"fmt"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func copyEnv(in map[string]string) map[string]string {
	out := make(map[string]string)
	for k, v := range in {
		out[k] = v
	}

	return out
}

func AddSnapEnvironment(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapEnvironmentDir, 0755); err != nil {
		return err
	}

	for _, app := range s.Apps {
		// init with global env, but per-app env wins on conflict
		appEnv := copyEnv(s.Environment)
		for k, v := range app.Environment {
			appEnv[k] = v
		}

		env := bytes.NewBuffer(nil)
		for k, v := range appEnv {
			fmt.Fprintf(env, "%s=%s\n", k, v)
		}

		if err := osutil.AtomicWriteFile(app.EnvironmentFile(), env.Bytes(), 0755, 0); err != nil {
			return err
		}
	}

	return nil
}

func RemoveSnapEnvironment(s *snap.Info) error {
	for _, app := range s.Apps {
		if err := os.Remove(app.EnvironmentFile()); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}
