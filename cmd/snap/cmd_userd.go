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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/userd"
)

type cmdUserd struct {
	userd userd.Userd

	Autostart bool `long:"autostart"`
}

var shortUserdHelp = i18n.G("Start the userd service")
var longUserdHelp = i18n.G("The userd command starts the snap user session service.")

func init() {
	cmd := addCommand("userd",
		shortUserdHelp,
		longUserdHelp,
		func() flags.Commander {
			return &cmdUserd{}
		}, map[string]string{
			"autostart": i18n.G("Autostart user applications"),
		}, nil)
	cmd.hidden = true
}

func (x *cmdUserd) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.Autostart {
		return x.runAutostart()
	}

	if err := x.userd.Init(); err != nil {
		return err
	}
	x.userd.Start()

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	select {
	case sig := <-ch:
		fmt.Fprintf(Stdout, "Exiting on %s.\n", sig)
	case <-x.userd.Dying():
		// something called Stop()
	}

	return x.userd.Stop()
}

var (
	replacedDesktopKeys = []string{"%f", "%F", "%u", "%U", "%d", "%D",
		"%n", "%N", "%i", "%c", "%k", "%v", "%m"}
)

func findExec(desktopFilePath string) (string, error) {
	f, err := os.Open(desktopFilePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}

		execCmd := strings.Split(line, "Exec=")[1]
		for _, key := range replacedDesktopKeys {
			execCmd = strings.Replace(execCmd, key, "", -1)
		}
		return execCmd, nil
	}
	return "", nil
}

func (x *cmdUserd) runAutostart() error {
	logger.Debugf("autostart")
	usr, err := user.Current()
	if err != nil {
		return err
	}

	usrSnapDir := filepath.Join(usr.HomeDir, "snap")

	glob := filepath.Join(usrSnapDir, "*/current/.config/autostart/*.desktop")
	matches, err := filepath.Glob(glob)
	if err != nil {
		return err
	}

	for _, desktopFile := range matches {
		logger.Debugf("autostart desktop file %v", desktopFile)
		// /home/foo/snap/some-snap/current/.config/autostart/some-app.desktop ->
		//    some-snap/current/.config/autostart/some-app.desktop
		noPrefix := strings.TrimPrefix(desktopFile, usrSnapDir+"/")
		// some-snap/current/.config/autostart/some-app.desktop -> some-snap
		snapName := noPrefix[0:strings.IndexByte(noPrefix, '/')]
		logger.Debugf("snap name: %v", snapName)
		info, err := getSnapInfo(snapName, snap.R(0))
		if err != nil {
			logger.Noticef("failed to obtain snap information for snap %q: %v", snapName, err)
			continue
		}
		// use the sanitized desktop file
		snapAppDesktop := filepath.Join(dirs.SnapDesktopFilesDir,
			fmt.Sprintf("%s_%s", info.Name(), filepath.Base(desktopFile)))
		command, err := findExec(snapAppDesktop)
		if err != nil {
			logger.Noticef("failed to locate app command for %v: %v", filepath.Base(desktopFile), err)
			continue
		}
		if command == "" {
			logger.Noticef("Exec= line not found in desktop file %v", snapAppDesktop)
			continue
		}
		logger.Debugf("exec line: %v", command)

		// TODO: shlex
		split := strings.Split(command, " ")
		cmd := exec.Command(split[0], split[1:]...)
		if err := cmd.Start(); err != nil {
			logger.Noticef("failed to start %q: %v", command, err)
		}
	}
	return nil
}
