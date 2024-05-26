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
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
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

const sshListenOpt = "service.ssh.listen-address"

func init() {
	for _, service := range services {
		s := fmt.Sprintf("core.service.%s.disable", service.configName)
		supportedConfigurations[s] = true
	}
	supportedConfigurations["core."+sshListenOpt] = true
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
		mylog.Check(os.MkdirAll(filepath.Join(rootDir, "/etc/ssh"), 0755))

	}

	sshCanary := filepath.Join(rootDir, "/etc/ssh/sshd_not_to_be_run")

	units := []string{serviceName}
	if disabled {
		mylog.Check(os.WriteFile(sshCanary, []byte("SSH has been disabled by snapd system configuration\n"), 0644))

		if opts == nil {
			return sysd.Stop(units)
		}
	} else {
		mylog.Check(os.Remove(sshCanary))
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
		modeenv := mylog.Check2(boot.ReadModeenv(dirs.GlobalRootDir))
		if err == nil && modeenv.Mode == boot.ModeInstall {
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
	mylog.Check(os.MkdirAll(filepath.Dir(consoleConfDisabled), 0755))
	mylog.Check(os.WriteFile(consoleConfDisabled, []byte("console-conf has been disabled by the snapd system configuration\n"), 0644))

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
		status := mylog.Check2(sysd.Status(units))

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
			mylog.Check(sysd.DisableNoReload(units))
		}
		mylog.Check(sysd.Mask(serviceName))

		// mask triggered a reload already
		if opts == nil {
			return sysd.Stop(units)
		}
	} else {
		mylog.Check(sysd.Unmask(serviceName))

		if opts == nil {
			mylog.Check(sysd.EnableNoReload(units))
			mylog.Check(

				// enable does not trigger reloads, so issue one now
				sysd.DaemonReload())

		}
		if opts == nil {
			return sysd.Start(units)
		}
	}
	return nil
}

// services that can be disabled
func handleServiceConfiguration(dev sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	// deal with service disable
	for _, service := range services {
		optionName := fmt.Sprintf("service.%s.disable", service.configName)
		outputStr := mylog.Check2(coreCfg(tr, optionName))

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
			mylog.Check(switchDisableService(service.systemdName, disabled, opts))

		}
	}
	mylog.Check(
		// configure ssh ports
		handleServiceConfigSSHListen(dev, tr, opts))

	return nil
}

func parseOneSSHListenAddr(oneAddr string) (addrs []string, err error) {
	// 1. check if it's something like "host:port", "[host]:port" etc
	//    This will return an error if there is no port specified so
	//    on error it's assume there is no port.
	host, portStr := mylog.Check3(net.SplitHostPort(oneAddr))

	// for any error assume there is no port and continue

	// 2. valid port (if needed)
	if portStr != "" {
		port := mylog.Check2(strconv.Atoi(portStr))

		if port > 65535 || port < 1 {
			return nil, fmt.Errorf("port %v must be in the range 1-65535", port)
		}
	}
	// 3. validate host
	if host != "" {
		if net.ParseIP(host) == nil && validateHostname(host) != nil {
			return nil, fmt.Errorf("invalid hostname %q", host)
		}
	}

	// at this point the oneAddr is validated but openssh will
	// error when no host is given, so workaround here
	if host == "" && portStr != "" {
		return []string{
			fmt.Sprintf("0.0.0.0:%v", portStr),
			fmt.Sprintf("[::]:%v", portStr),
		}, nil
	}

	// no special handling needed and a valid listen address
	return []string{oneAddr}, nil
}

func parseSSHListenCfg(cfgStr string) ([]string, error) {
	var listenAddrs []string
	for _, hostAndPort := range strings.Split(cfgStr, ",") {
		addrs := mylog.Check2(parseOneSSHListenAddr(hostAndPort))

		listenAddrs = append(listenAddrs, addrs...)
	}
	return listenAddrs, nil
}

func validateServiceConfiguration(tr ConfGetter) error {
	// validate the ssh listen setting
	output := mylog.Check2(coreCfg(tr, sshListenOpt))

	if output == "" {
		return nil
	}
	mylog.Check2(parseSSHListenCfg(output))

	return nil
}

func handleServiceConfigSSHListen(dev sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	// see if anything needs to happen
	var pristineSSHListen, newSSHListen interface{}

	if mylog.Check(tr.GetPristine("core", sshListenOpt, &pristineSSHListen)); err != nil && !config.IsNoOption(err) {
		return err
	}
	if mylog.Check(tr.Get("core", sshListenOpt, &newSSHListen)); err != nil && !config.IsNoOption(err) {
		return err
	}
	if pristineSSHListen == newSSHListen {
		return nil
	}

	// ssh.port config has changed, write new config
	root := dirs.GlobalRootDir
	if opts != nil {
		root = opts.RootDir
	}

	// Note: Only UC20+ supports using the "sshd_config.d"
	// dir. Supporting older systems would be hard because we
	// would have to merge somehow the UC16 and UC18 config in a
	// fsOnlyContext
	if !dev.HasModeenv() {
		return fmt.Errorf("cannot set ssh listen address configuration on systems older than UC20")
	}

	name := "listen.conf"
	dirContent := map[string]osutil.FileState{}
	if newSSHListen != nil && newSSHListen != "" {
		listenAddrs := mylog.Check2(parseSSHListenCfg(fmt.Sprintf("%v", newSSHListen)))

		var buf bytes.Buffer
		for _, s := range listenAddrs {
			mylog.Check2(fmt.Fprintf(&buf, "ListenAddress %v\n", s))
		}

		dirContent[name] = &osutil.MemoryFileState{
			Content: buf.Bytes(),
			Mode:    0600,
		}
	}
	dir := filepath.Join(root, "/etc/ssh/sshd_config.d/")
	mylog.Check(os.MkdirAll(dir, 0755))

	_, _ := mylog.Check3(osutil.EnsureDirState(dir, name, dirContent))

	if opts == nil {
		sysd := systemd.New(systemd.SystemMode, &sysdLogger{})
		mylog.Check(sysd.ReloadOrRestart([]string{"ssh.service"}))

	}

	return nil
}
