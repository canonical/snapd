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

package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/i18n"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

type cmdService struct {
	Status  svcStatus  `command:"status"`
	Start   svcStart   `command:"start"`
	Stop    svcStop    `command:"stop"`
	Restart svcRestart `command:"restart"`
	Enable  svcEnable  `command:"enable"`
	Disable svcDisable `command:"disable"`
	Logs    svcLogs    `command:"logs"`
}

type svcBase struct {
	Args struct {
		Snap    string `positional-arg-name:"snap"`
		Service string `positional-arg-name:"service"`
	} `positional-args:"yes"`
}

type svcStatus struct{ svcBase }
type svcStart struct{ svcBase }
type svcStop struct{ svcBase }
type svcRestart struct{ svcBase }
type svcEnable struct{ svcBase }
type svcDisable struct{ svcBase }
type svcLogs struct{ svcBase }

func init() {
	_, err := parser.AddCommand("service",
		i18n.G("Query and modify snappy services"),
		i18n.G("Query and modify snappy services of locally-installed packages"),
		&cmdService{})

	if err != nil {
		logger.Panicf("Unable to service: %v", err)
	}

}

const (
	doStatus = iota
	doStart
	doStop
	doRestart
	doEnable
	doDisable
	doLogs
)

func (s *svcBase) doExecute(cmd int) ([]string, error) {
	actor, err := snappy.FindServices(s.Args.Snap, s.Args.Service, progress.MakeProgressBar())
	if err != nil {
		return nil, err
	}

	switch cmd {
	case doStatus:
		return actor.Status()
	case doLogs:
		return actor.Loglines()
	case doStart:
		return nil, actor.Start()
	case doStop:
		return nil, actor.Stop()
	case doRestart:
		return nil, actor.Restart()
	case doEnable:
		return nil, actor.Enable()
	case doDisable:
		return nil, actor.Disable()
	default:
		panic("can't happen")
	}
}

func (s *svcStatus) Execute(args []string) error {
	stati, err := s.doExecute(doStatus)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 1, '\t', 0)

	ws, _ := helpers.GetTermWinsize()
	rows := int(ws.Row) - 2

	header := i18n.G("Snap\tService\tState")

	for i, status := range stati {
		// print a header every $rows rows if rows is bigger
		// than 10; otherwise, just the once. 10 is arbitrary,
		// but the thinking is that on the one hand you don't
		// want to be using up too much space with headers on
		// really small terminals and on the other rows might
		// actually be negative if you're not on a tty.
		if i%rows == 0 && (i == 0 || rows > 10) {
			fmt.Fprintln(w, header)
		}
		fmt.Fprintln(w, status)
	}
	w.Flush()

	return err
}

func (s *svcLogs) Execute([]string) error {
	return withMutexAndRetry(func() error {
		logs, err := s.doExecute(doLogs)
		if err != nil {
			return err
		}

		for i := range logs {
			fmt.Println(logs[i])
		}

		return nil
	})
}

func (s *svcStart) Execute(args []string) error {
	return withMutexAndRetry(func() error {
		_, err := s.doExecute(doStart)
		return err
	})
}

func (s *svcStop) Execute(args []string) error {
	return withMutexAndRetry(func() error {
		_, err := s.doExecute(doStop)
		return err
	})
}

func (s *svcRestart) Execute(args []string) error {
	return withMutexAndRetry(func() error {
		_, err := s.doExecute(doRestart)
		return err
	})
}

func (s *svcEnable) Execute(args []string) error {
	return withMutexAndRetry(func() error {
		_, err := s.doExecute(doEnable)
		return err
	})
}

func (s *svcDisable) Execute(args []string) error {
	return withMutexAndRetry(func() error {
		_, err := s.doExecute(doDisable)
		return err
	})
}
