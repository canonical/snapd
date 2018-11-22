// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package backend

import (
	"fmt"

	"github.com/snapcore/snapd/osutil"
)

var updateFontconfigCaches = updateFontconfigCachesImpl

// updateFontconfigCaches always update the fontconfig caches
func updateFontconfigCachesImpl() error {
	for _, fc := range []string{"fc-cache-v6", "fc-cache-v7"} {
		// FIXME: also use the snapd snap if available
		cmd, err := osutil.CommandFromCore("/bin/" + fc)
		if err != nil {
			return fmt.Errorf("cannot get %s from core: %v", fc, err)
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cannot run %s on core: %v", fc, osutil.OutputErr(output, err))
		}
	}
	return nil
}
