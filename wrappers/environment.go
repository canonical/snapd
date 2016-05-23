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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

func AddSnapEnvironment(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapEnvironmentDir, 0755); err != nil {
		return err
	}

	globalEnv := bytes.NewBuffer(nil)
	for k, v := range s.Environment {
		fmt.Fprintf(globalEnv, "%s=%s\n", k, v)
	}

	for _, app := range s.Apps {
		// FIXME: add support for per-app specific environment map
		//        in snap.yaml too

		if err := osutil.AtomicWriteFile(app.EnvironmentFile(), globalEnv.Bytes(), 0755, 0); err != nil {
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
