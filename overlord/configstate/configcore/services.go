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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

var services = []struct{ configName, systemdName string }{
	{"ssh", "ssh.service"},
	{"rsyslog", "rsyslog.service"},
	{"console-conf", "console-conf@*"},
	{"systemd-resolved", "systemd-resolved.service"},
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

	units := []string{serviceName}
	if disabled {
		if err := ioutil.WriteFile(sshCanary, []byte("SSH has been disabled by snapd system configuration\n"), 0644); err != nil {
			return err
		}
		if opts == nil {
			return sysd.Stop(units)
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
			return sysd.Start(units)
		}
	}
	return nil
}

// switchDisableConsoleConfService handles the special case of
// disabling/enabling console-conf on core devices.
//
// Note that this option can only be changed via gadget defaults.
// It is not possible to tune this at runtime
func switchDisableConsoleConfService(sysd systemd.Systemd, serviceName string, disabled bool, opts *fsOnlyContext) error {
	consoleConfDisabled := "/var/lib/console-conf/complete"

	// at runtime we can not change this setting
	if opts == nil {

		// Special case: during install mode the
		// gadget-defaults will also be set as part of the
		// system install change. However during install mode
		// console-conf has no "complete" file, it just never runs
		// in install mode. So we need to detect this and do nothing
		// or the install mode will fail.
		// XXX: instead of this hack we should look at the config
		//      defaults and compare with the setting and exit if
		//      they are the same but that requires some more changes.
		// TODO: leverage sysconfig.Device instead
		mode, _, _ := boot.ModeAndRecoverySystemFromKernelCommandLine()
		if mode == boot.ModeInstall {
			return nil
		}

		hasDisabledFile := osutil.FileExists(filepath.Join(dirs.GlobalRootDir, consoleConfDisabled))
		if disabled != hasDisabledFile {
			return fmt.Errorf("cannot toggle console-conf at runtime, but only initially via gadget defaults")
		}
		return nil
	}

	if !disabled {
		return nil
	}

	// disable console-conf at the gadget-defaults time
	consoleConfDisabled = filepath.Join(opts.RootDir, consoleConfDisabled)
	if err := os.MkdirAll(filepath.Dir(consoleConfDisabled), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(consoleConfDisabled, []byte("console-conf has been disabled by the snapd system configuration\n"), 0644); err != nil {
		return err
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
		sysd = systemd.New(systemd.SystemMode, &sysdLogger{})
	}

	// some services are special
	switch serviceName {
	case "ssh.service":
		return switchDisableSSHService(sysd, serviceName, disabled, opts)
	case "console-conf@*":
		return switchDisableConsoleConfService(sysd, serviceName, disabled, opts)
	}

	units := []string{serviceName}
	if opts == nil {
		// ignore the service if not installed
		status, err := sysd.Status(units)
		if err != nil {
			return err
		}
		if len(status) != 1 {
			return fmt.Errorf("internal error: expected status of service %s, got %v", serviceName, status)
		}
		if !status[0].Installed {
			// ignore
			return nil
		}
	}

	if disabled {
		if opts == nil {
			if err := sysd.DisableNoReload(units); err != nil {
				return err
			}
		}
		if err := sysd.Mask(serviceName); err != nil {
			return err
		}
		// mask triggered a reload already
		if opts == nil {
			return sysd.Stop(units)
		}
	} else {
		if err := sysd.Unmask(serviceName); err != nil {
			return err
		}
		if opts == nil {
			if err := sysd.EnableNoReload(units); err != nil {
				return err
			}
			// enable does not trigger reloads, so issue one now
			if err := sysd.DaemonReload(); err != nil {
				return err
			}
		}
		if opts == nil {
			return sysd.Start(units)
		}
	}
	return nil
}

// services that can be disabled
func handleServiceDisableConfiguration(_ sysconfig.Device, tr config.ConfGetter, opts *fsOnlyContext) error {
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
