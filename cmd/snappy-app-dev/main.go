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

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

var deviceCgroupDir = `/sys/fs/cgroup/devices`

func verifyAction(action string) (string, error) {
	switch action {
	case
		"add",
		"change",
		"remove":
		return action, nil
	}
	return "", fmt.Errorf("unsupported action %q", action)
}

func verifyAppname(appname string) (string, error) {
	tmp := strings.Split(appname, "_")
	if len(tmp) != 3 || tmp[0] != "snap" {
		return "", fmt.Errorf("appname should be snap_NAME_COMMAND")
	}

	err := snap.ValidateName(tmp[1])
	if err != nil {
		return "", err
	}

	err = snap.ValidateAppName(tmp[2])
	if err != nil {
		return "", err
	}

	// appname comes in as snap_foo_bar, but the cgroup uses snap.foo.bar
	return strings.Replace(appname, "_", ".", -1), nil
}

func verifyDevPath(devpath string) (string, error) {
	if devpath != path.Clean(devpath) {
		return "", fmt.Errorf("invalid DEVPATH %q", devpath)
	} else if !strings.HasPrefix(devpath, "/") {
		return "", fmt.Errorf("DEVPATH should start with /")
	}
	return devpath, nil
}

func verifyMajorMinor(majmin string) (string, error) {
	tmp := strings.Split(majmin, ":")
	if len(tmp) != 2 {
		return "", fmt.Errorf("should be MAJOR:MINOR")
	}

	for _, val := range tmp {
		if _, err := strconv.ParseUint(val, 10, 32); err != nil {
			return "", fmt.Errorf("MAJOR and MINOR should be uint32")
		}
	}
	return majmin, nil
}

func getAcl(devpath string, majmin string) (string, error) {
	devpath, err := verifyDevPath(devpath)
	if err != nil {
		return "", err
	}

	majmin, err = verifyMajorMinor(majmin)
	if err != nil {
		return "", err
	}

	devType := "c"
	if strings.Contains(devpath, "/block/") {
		devType = "b"
	}

	return fmt.Sprintf("%s %s rwm", devType, majmin), nil
}

func getDeviceCgroupFn(action string, name string) (string, error) {
	cmd, err := verifyAction(action)
	if err != nil {
		return "", err
	}

	appname, err := verifyAppname(name)
	if err != nil {
		return "", err
	}

	if cmd == "remove" {
		return fmt.Sprintf(path.Join(deviceCgroupDir, appname, "devices.deny")), nil
	}
	return fmt.Sprintf(path.Join(deviceCgroupDir, appname, "devices.allow")), nil
}

func writeAcl(path string, acl string) error {
	if err := ioutil.WriteFile(path, []byte(acl+"\n"), 0644); err != nil {
		return err
	}
	return nil
}

var initLogger = realInitLogger

func realInitLogger() error {
	// Setup syslogger for easier debugging with udev
	sysLog, err := syslog.Dial("", "", syslog.LOG_INFO, "snappy-app-dev")
	if err != nil {
		return fmt.Errorf("error: failed to setup syslog: %v\n", err)
	}

	l, err := logger.New(sysLog, log.Lshortfile)
	if err != nil {
		return fmt.Errorf("error: failed to activate logging: %v\n", err)
	}
	logger.SetLogger(l)

	return nil
}

func run(args []string) error {
	err := initLogger()
	if err != nil {
		return err
	}

	if len(args) != 5 {
		logger.Panicf("%s ACTION APPNAME DEVPATH MAJOR:MINOR\n",
			path.Base(args[0]))
	}

	fn, err := getDeviceCgroupFn(args[1], args[2])
	if err != nil {
		logger.Panicf("%v\n", err)
	}

	acl, err := getAcl(args[3], args[4])
	if err != nil {
		logger.Panicf("%v\n", err)
	}

	logger.Debugf("env=%v\n", os.Environ())
	logger.Debugf("writing 'acl=%v' to '%v'", acl, fn)
	if err := writeAcl(fn, acl); err != nil {
		logger.Panicf("%v\n", err)
	}

	return nil
}

/*
 * udev callout to update per-snap device cgroups for a device node. This is
 * called by both snap-confine on app invocation and by udev when a device
 * matching the snap's tag is hotplugged/unplugged (note that udev requires us
 * to substitute '_' for '.'. Eg:
 *  TAG=="snap_foo_bar", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_foo_bar $devpath $major:$minor"
 */
func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
