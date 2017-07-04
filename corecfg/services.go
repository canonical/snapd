// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg

import (
	"fmt"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
)

type sysdLogger struct{}

func (l *sysdLogger) Notify(status string) {
	fmt.Fprintf(Stderr, "sysd: %s\n", status)
}

// swtichDisableService switches a service in/out of disabled state
// where "true" means disabled and "false" means enabled.
func switchDisableService(service, value string) error {
	sysd := systemd.New(dirs.GlobalRootDir, &sysdLogger{})
	serviceName := fmt.Sprintf("%s.service", service)

	switch value {
	case "true":
		if err := sysd.Disable(serviceName); err != nil {
			return err
		}
		return sysd.Stop(serviceName, 5*time.Minute)
	case "false":
		if err := sysd.Enable(serviceName); err != nil {
			return err
		}
		return sysd.Start(serviceName)
	default:
		return fmt.Errorf("Invalid value %q provided for option %q", value, serviceName)
	}
}
