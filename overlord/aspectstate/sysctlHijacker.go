// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

package aspectstate

import (
	"os/exec"
	"strings"
)

func init() {
	Hijack("system", "sysctl", "sysctl", SysctlHijacker{})
}

type SysctlHijacker struct {
	BaseHijacker
}

func (SysctlHijacker) Get(path string, value interface{}) error {
	var cmd *exec.Cmd
	if path == "all" {
		cmd = exec.Command("sysctl", "-a")
	} else {
		cmd = exec.Command("sysctl", path)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	if path == "all" {
		*value.(*interface{}) = string(out)
	} else {
		parts := strings.Split(string(out), "=")
		*value.(*interface{}) = strings.TrimSpace(parts[1])
	}

	return nil
}
