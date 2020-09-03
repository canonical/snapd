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

package configcore

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/systemd"
)

var services = []struct{ configName, systemdName string }{
	{"ssh", "ssh.service"},
	{"rsyslog", "rsyslog.service"},
	{"console-conf", "console-conf@*"},
}

func init() {
	for _, service := range services {
		s := fmt.Sprintf("core.service.%s.disable", service.configName)
		supportedConfigurations[s] = true
	}
}

type sysdLogger struct{}

func (l *sysdLogger) Notify(status string) {
	fmt.Fprintf(Stderr, "sysd: %s\n", status)
}

// switchDisableSSHService handles the special case of disabling/enabling ssh
// service on core devices.
func switchDisableSSHService(sysd systemd.Systemd, serviceName string, disabled bool, opts *fsOnlyContext) error {
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
		if err := os.MkdirAll(filepath.Join(rootDir, "/etc/ssh"), 0755); err != nil {
			return err
		}
	}

	sshCanary := filepath.Join(rootDir, "/etc/ssh/sshd_not_to_be_run")

	if disabled {
		if err := ioutil.WriteFile(sshCanary, []byte("SSH has been disabled by snapd system configuration\n"), 0644); err != nil {
			return err
		}
		if opts == nil {
			return sysd.Stop(serviceName, 5*time.Minute)
		}
	} else {
		err := os.Remove(sshCanary)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		// Unmask both sshd.service and ssh.service and ignore the
		// errors, if any. This undoes the damage done by earlier
		// versions of snapd.
		sysd.Unmask("sshd.service")
		sysd.Unmask("ssh.service")
		if opts == nil {
			return sysd.Start(serviceName)
		}
	}
	return nil
}

// switchDisableConsoleConfService handles the special case of disabling/enabling
// console-conf on core devices.
//
// The command sequence that works to start/stop console-conf after setting
// the marker file in /var/lib/console-conf/complete is:
//
//     systemctl restart 'getty@*' --all
//     systemctl restart 'serial-getty@*' --all
//     systemctl restart 'serial-console-conf@*' --all
//     systemctl restart 'console-conf@*' --all
//
// This restarts all active getty and console-conf instances, even
// ones that were started on-demand (eg. on tty2)
func switchDisableConsoleConfService(sysd systemd.Systemd, serviceName string, disabled bool, opts *fsOnlyContext) error {
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "/var/lib/console-conf"), 0755); err != nil {
		return err
	}

	consoleConfCanary := filepath.Join(rootDir, "/var/lib/console-conf/complete")

	restartServicesOnTTYs := func() error {
		// getty@ and console-conf@ are template services, that only
		// exist when an instance is active, typically in a UC20 image
		// only getty@tty1 is defined as a side effect of being 'wanted'
		// by the getty.target;
		// restarting all console-conf@* units ensures on-demand units
		// started on other ttys are affected too
		if err := sysd.RestartAll("getty@*"); err != nil {
			return err
		}
		if err := sysd.RestartAll("serial-getty@*"); err != nil {
			return err
		}
		if err := sysd.RestartAll("serial-console-conf@*"); err != nil {
			return err
		}
		return sysd.RestartAll("console-conf@*")
	}

	if disabled {
		if err := ioutil.WriteFile(consoleConfCanary, []byte("console-conf has been disabled by snapd system configuration\n"), 0644); err != nil {
			return err
		}
		if opts == nil {
			return restartServicesOnTTYs()
		}
	} else {
		err := os.Remove(consoleConfCanary)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			// no need to restart the services
			return nil
		}
		if opts == nil {
			return restartServicesOnTTYs()
		}
	}
	return nil
}

// switchDisableTypicalService switches a service in/out of disabled state
// where "true" means disabled and "false" means enabled.
func switchDisableService(serviceName string, disabled bool, opts *fsOnlyContext) error {
	var sysd systemd.Systemd
	if opts != nil {
		sysd = systemd.NewEmulationMode(opts.RootDir)
	} else {
		sysd = systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.SystemMode, &sysdLogger{})
	}

	// some services are special
	switch serviceName {
	case "ssh.service":
		return switchDisableSSHService(sysd, serviceName, disabled, opts)
	case "console-conf@*":
		return switchDisableConsoleConfService(sysd, serviceName, disabled, opts)
	}

	if disabled {
		if opts == nil {
			if err := sysd.Disable(serviceName); err != nil {
				return err
			}
		}
		if err := sysd.Mask(serviceName); err != nil {
			return err
		}
		if opts == nil {
			return sysd.Stop(serviceName, 5*time.Minute)
		}
	} else {
		if err := sysd.Unmask(serviceName); err != nil {
			return err
		}
		if opts == nil {
			if err := sysd.Enable(serviceName); err != nil {
				return err
			}
		}
		if opts == nil {
			return sysd.Start(serviceName)
		}
	}
	return nil
}

// services that can be disabled
func handleServiceDisableConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	for _, service := range services {
		optionName := fmt.Sprintf("service.%s.disable", service.configName)
		outputStr, err := coreCfg(tr, optionName)
		if err != nil {
			return err
		}
		if outputStr != "" {
			var disabled bool
			switch outputStr {
			case "true":
				disabled = true
			case "false":
				disabled = false
			default:
				return fmt.Errorf("option %q has invalid value %q", optionName, outputStr)
			}

			if err := switchDisableService(service.systemdName, disabled, opts); err != nil {
				return err
			}
		}
	}

	return nil
}
