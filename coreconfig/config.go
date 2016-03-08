// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package coreconfig

import (
	"bufio"
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/systemd"

	"gopkg.in/yaml.v2"
)

const (
	tzPathEnvironment string = "UBUNTU_CORE_CONFIG_TZ_FILE"
	tzPathDefault     string = "/etc/timezone"
)

var (
	tzZoneInfoPath   = "/usr/share/zoneinfo"
	tzZoneInfoTarget = "/etc/localtime"
)

const (
	autoUpdateTimer         string = "snappy-autopilot.timer"
	autoUpdateTimerEnabled  string = "enabled"
	autoUpdateTimerDisabled string = "disabled"
)

var (
	modprobePath        = "/etc/modprobe.d/ubuntu-core.conf"
	modulesPath         = "/etc/modules-load.d/ubuntu-core.conf"
	interfacesRoot      = "/etc/network/interfaces.d/"
	pppRoot             = "/etc/ppp/"
	watchdogConfigPath  = "/etc/watchdog.conf"
	watchdogStartupPath = "/etc/default/watchdog"
)

var (
	// ErrInvalidUnitStatus signals that a unit is not returning a status
	// of "enabled" or "disabled".
	ErrInvalidUnitStatus = errors.New("invalid unit status")

	// ErrInvalidConfig is returned from Set when the value
	// provided is not a valid configuration string.
	ErrInvalidConfig = errors.New("invalid ubuntu-core configuration")
)

type systemConfig struct {
	AutoUpdate *bool           `yaml:"autoupdate,omitempty"`
	Timezone   *string         `yaml:"timezone,omitempty"`
	Hostname   *string         `yaml:"hostname,omitempty"`
	Modprobe   *string         `yaml:"modprobe,omitempty"`
	Modules    []string        `yaml:"load-kernel-modules,omitempty"`
	Network    *networkConfig  `yaml:"network,omitempty"`
	Watchdog   *watchdogConfig `yaml:"watchdog,omitempty"`
}

type networkConfig struct {
	Interfaces []passthroughConfig `yaml:"interfaces"`
	PPP        []passthroughConfig `yaml:"ppp"`
}

type passthroughConfig struct {
	Name    string `yaml:"name,omitempty"`
	Content string `yaml:"content,omitempty"`
}

type watchdogConfig struct {
	Startup string `yaml:"startup,omitempty"`
	Config  string `yaml:"config,omitempty"`
}

type coreConfig struct {
	UbuntuCore *systemConfig `yaml:"ubuntu-core"`
}

type configYaml struct {
	Config coreConfig
}

func newSystemConfig() (*systemConfig, error) {
	// TODO think of a smart way not to miss a config entry
	tz, err := getTimezone()
	if err != nil {
		return nil, err
	}

	autoUpdate, err := getAutoUpdate()
	if err != nil {
		return nil, err
	}
	hostname, err := getHostname()
	if err != nil {
		return nil, err
	}
	modprobe, err := getModprobe()
	if err != nil {
		return nil, err
	}
	modules, err := getModules()
	if err != nil {
		return nil, err
	}
	interfaces, err := getInterfaces()
	if err != nil {
		return nil, err
	}
	ppp, err := getPPP()
	if err != nil {
		return nil, err
	}
	watchdog, err := getWatchdog()
	if err != nil {
		return nil, err
	}

	var network *networkConfig
	if len(interfaces) > 0 || len(ppp) > 0 {
		network = &networkConfig{
			Interfaces: interfaces,
			PPP:        ppp,
		}
	}

	config := &systemConfig{
		AutoUpdate: &autoUpdate,
		Timezone:   &tz,
		Hostname:   &hostname,
		Modprobe:   &modprobe,
		Modules:    modules,
		Network:    network,
		Watchdog:   watchdog,
	}

	return config, nil
}

// for testing purposes
var yamlMarshal = yaml.Marshal

// Get is a special configuration case for the system, for which
// there is no such entry in a snap.yaml to satisfy the snappy config interface.
// This implements getting the current configuration for ubuntu-core.
func Get() (rawConfig []byte, err error) {
	config, err := newSystemConfig()
	if err != nil {
		return nil, err
	}

	out, err := yamlMarshal(&configYaml{Config: coreConfig{config}})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func passthroughEqual(a, b []passthroughConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}

	return true
}

// Set is used to configure settings for the system, this is meant to
// be used as an interface for snappy config to satisfy the ubuntu-core
// hook.
func Set(rawConfig []byte) (newRawConfig []byte, err error) {
	oldConfig, err := newSystemConfig()
	if err != nil {
		return nil, err
	}

	var configWrap configYaml
	err = yaml.Unmarshal(rawConfig, &configWrap)
	if err != nil {
		return nil, err
	}
	newConfig := configWrap.Config.UbuntuCore
	if newConfig == nil {
		return nil, ErrInvalidConfig
	}

	needsModReload := false

	rNewConfig := reflect.ValueOf(newConfig).Elem()
	rType := rNewConfig.Type()
	for i := 0; i < rNewConfig.NumField(); i++ {
		if rNewConfig.Field(i).IsNil() {
			continue
		}

		switch rType.Field(i).Name {
		case "Timezone":
			if *oldConfig.Timezone == *newConfig.Timezone {
				continue
			}

			if err := setTimezone(*newConfig.Timezone); err != nil {
				return nil, err
			}
		case "AutoUpdate":
			if *oldConfig.AutoUpdate == *newConfig.AutoUpdate {
				continue
			}

			if err := setAutoUpdate(*newConfig.AutoUpdate); err != nil {
				return nil, err
			}
		case "Hostname":
			if *oldConfig.Hostname == *newConfig.Hostname {
				continue
			}

			if err := setHostname(*newConfig.Hostname); err != nil {
				return nil, err
			}
		case "Modprobe":
			if *oldConfig.Modprobe == *newConfig.Modprobe {
				continue
			}

			if err := setModprobe(*newConfig.Modprobe); err != nil {
				return nil, err
			}
			needsModReload = true
		case "Modules":
			if err := setModules(newConfig.Modules); err != nil {
				return nil, err
			}
			needsModReload = true
		case "Network":
			if oldConfig.Network == nil || !passthroughEqual(oldConfig.Network.Interfaces, newConfig.Network.Interfaces) {
				if err := setInterfaces(newConfig.Network.Interfaces); err != nil {
					return nil, err
				}
			}
			if oldConfig.Network == nil || !passthroughEqual(oldConfig.Network.PPP, newConfig.Network.PPP) {
				if err := setPPP(newConfig.Network.PPP); err != nil {
					return nil, err
				}
			}
		case "Watchdog":
			if oldConfig.Watchdog != nil && *oldConfig.Watchdog == *newConfig.Watchdog {
				continue
			}

			if err := setWatchdog(newConfig.Watchdog); err != nil {
				return nil, err
			}
		}
	}

	if needsModReload {
		if _, err := systemd.SystemctlCmd("restart", "--no-block", "systemd-modules-load.service"); err != nil {
			return nil, err
		}
	}

	return Get()
}

// tzFile determines which timezone file to read from
func tzFile() string {
	tzFile := os.Getenv(tzPathEnvironment)
	if tzFile == "" {
		tzFile = tzPathDefault
	}

	return tzFile
}

// getTimezone returns the current timezone the system is set to or an error
// if it can't.
var getTimezone = func() (timezone string, err error) {
	tz, err := ioutil.ReadFile(tzFile())
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(tz)), nil
}

// setTimezone sets the specified timezone for the system, an error is returned
// if it can't.
var setTimezone = func(timezone string) error {
	if err := osutil.CopyFile(filepath.Join(tzZoneInfoPath, timezone), tzZoneInfoTarget, osutil.CopyFlagOverwrite); err != nil {
		return err
	}

	return osutil.AtomicWriteFile(tzFile(), []byte(timezone), 0644, osutil.AtomicWriteFollow)
}

func getPassthrough(rootDir string) (pc []passthroughConfig, err error) {
	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		content, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		pc = append(pc, passthroughConfig{
			Name:    path[len(rootDir):],
			Content: string(content),
		})
		return nil
	})

	return pc, nil

}
func setPassthrough(rootDir string, pc []passthroughConfig) error {
	for _, c := range pc {
		path := filepath.Join(rootDir, c.Name)
		if c.Content == "" {
			os.Remove(path)
			continue
		}
		if err := osutil.AtomicWriteFile(path, []byte(c.Content), 0644, osutil.AtomicWriteFollow); err != nil {
			return err
		}
	}

	return nil
}

var getInterfaces = func() (pc []passthroughConfig, err error) {
	return getPassthrough(interfacesRoot)
}

var setInterfaces = func(pc []passthroughConfig) error {
	return setPassthrough(interfacesRoot, pc)
}

var getPPP = func() (pc []passthroughConfig, err error) {
	return getPassthrough(pppRoot)
}

var setPPP = func(pc []passthroughConfig) error {
	return setPassthrough(pppRoot, pc)
}

// getModprobe returns the current modprobe config
var getModprobe = func() (string, error) {
	modprobe, err := ioutil.ReadFile(modprobePath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	return string(modprobe), nil
}

// setModprobe sets the specified modprobe config
var setModprobe = func(modprobe string) error {
	return osutil.AtomicWriteFile(modprobePath, []byte(modprobe), 0644, osutil.AtomicWriteFollow)
}

func getModules() ([]string, error) {
	f, err := os.Open(modulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	// there's a warning at the top of the file
	// but you know they're just going to edit it anyway
	// so be kind

	var modules []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}
		if line[0] == '#' || line[0] == ';' {
			continue
		}

		modules = append(modules, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// doing the sort on get makes testing easier
	sort.Strings(modules)

	return modules, nil
}

const modulesHeader = `#
# DO NOT EDIT THIS FILE
# it is auto-generated, and will be overwritten.
`

func setModules(modules []string) error {
	oldModules, err := getModules()
	if err != nil {
		return err
	}

	for i := range modules {
		m := strings.TrimSpace(modules[i])
		if len(m) == 0 {
			continue
		}

		if m[0] == '-' {
			m = m[1:]
			idx := sort.SearchStrings(oldModules, m)
			if idx == len(oldModules) || oldModules[idx] != m {
				// not found
				continue
			}
			oldModules = append(oldModules[:idx], oldModules[idx+1:]...)
		} else {
			idx := sort.SearchStrings(oldModules, m)
			if idx < len(oldModules) && oldModules[idx] == m {
				// already got it
				continue
			}
			oldModules = append(oldModules, "")
			copy(oldModules[idx+1:], oldModules[idx:])
			oldModules[idx] = m
		}
	}

	var buf bytes.Buffer

	// bytes' Write* methods always return nil error
	buf.WriteString(modulesHeader)

	for i := range oldModules {
		buf.WriteString(oldModules[i])
		buf.WriteByte('\n')
	}

	return osutil.AtomicWriteFile(modulesPath, buf.Bytes(), 0644, osutil.AtomicWriteFollow)
}

// getWatchdog returns the current watchdog config
var getWatchdog = func() (*watchdogConfig, error) {
	startup, err := ioutil.ReadFile(watchdogStartupPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	config, err := ioutil.ReadFile(watchdogConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// nil to make the yaml empty
	if len(startup) == 0 && len(config) == 0 {
		return nil, nil
	}

	return &watchdogConfig{
		Startup: string(startup),
		Config:  string(config)}, nil
}

// setWatchdog sets the specified watchdog config
var setWatchdog = func(wf *watchdogConfig) error {
	if err := osutil.AtomicWriteFile(watchdogStartupPath, []byte(wf.Startup), 0644, osutil.AtomicWriteFollow); err != nil {
		return err
	}

	return osutil.AtomicWriteFile(watchdogConfigPath, []byte(wf.Config), 0644, osutil.AtomicWriteFollow)
}

// for testing purposes
var (
	cmdAutoUpdateEnabled = []string{"is-enabled", autoUpdateTimer}
	cmdSystemctl         = "systemctl"
)

// getAutoUpdate returns the autoupdate state
var getAutoUpdate = func() (state bool, err error) {
	out, err := exec.Command(cmdSystemctl, cmdAutoUpdateEnabled...).Output()
	if exitErr, ok := err.(*exec.ExitError); ok {
		waitStatus := exitErr.Sys().(syscall.WaitStatus)

		// when a service is disabled the exit status is 1
		if e := waitStatus.ExitStatus(); e != 1 {
			return false, err
		}
	}

	status := strings.TrimSpace(string(out))

	if status == autoUpdateTimerEnabled {
		return true, nil
	} else if status == autoUpdateTimerDisabled {
		return false, nil
	} else {
		return false, ErrInvalidUnitStatus
	}
}

// for testing purposes
var (
	cmdEnableAutoUpdate  = []string{"enable", autoUpdateTimer}
	cmdStartAutoUpdate   = []string{"start", autoUpdateTimer}
	cmdDisableAutoUpdate = []string{"disable", autoUpdateTimer}
	cmdStopAutoUpdate    = []string{"stop", autoUpdateTimer}
)

// setAutoUpdate enables and starts, or stops and disables autoupdate
var setAutoUpdate = func(stateEnabled bool) error {
	if stateEnabled {
		if err := exec.Command(cmdSystemctl, cmdEnableAutoUpdate...).Run(); err != nil {
			return err
		}
		if err := exec.Command(cmdSystemctl, cmdStartAutoUpdate...).Run(); err != nil {
			return err
		}
	} else {
		if err := exec.Command(cmdSystemctl, cmdStopAutoUpdate...).Run(); err != nil {
			return err
		}
		if err := exec.Command(cmdSystemctl, cmdDisableAutoUpdate...).Run(); err != nil {
			return err
		}
	}

	return nil
}

// getHostname returns the hostname for the host
var getHostname = os.Hostname

var hostnamePath = "/etc/writable/hostname"
var syscallSethostname = syscall.Sethostname

// setHostname sets the hostname for the host
var setHostname = func(hostname string) error {
	hostnameB := []byte(hostname)

	if err := syscallSethostname(hostnameB); err != nil {
		return err
	}

	return osutil.AtomicWriteFile(hostnamePath, hostnameB, 0644, osutil.AtomicWriteFollow)
}
