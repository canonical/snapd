// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/triggerwatch"
	"github.com/snapcore/snapd/logger"
)

func init() {
	const (
		short = "Detect Ubuntu Core recovery chooser trigger"
		long  = ""
	)

	addCommandBuilder(func(parser *flags.Parser) {
		mylog.Check2(parser.AddCommand("recovery-chooser-trigger", short, long, &cmdRecoveryChooserTrigger{}))
	})
}

var (
	triggerwatchWait = triggerwatch.Wait

	// default trigger wait timeout
	defaultTimeout       = 10 * time.Second
	defaultDeviceTimeout = 2 * time.Second

	// default marker file location
	defaultMarkerFile = "/run/snapd-recovery-chooser-triggered"
)

type cmdRecoveryChooserTrigger struct {
	MarkerFile    string `long:"marker-file" value-name:"filename" description:"trigger marker file location"`
	WaitTimeout   string `long:"wait-timeout" value-name:"duration" description:"trigger wait timeout"`
	DeviceTimeout string `long:"device-timeout" value-name:"duration" description:"timeout for devices to appear"`
}

func (c *cmdRecoveryChooserTrigger) Execute(args []string) error {
	// TODO:UC20: check in the gadget if there is a hook or some binary we
	// should run for trigger detection. This will require some design work
	// and also thinking if/how such a hook can be confined.

	timeout := defaultTimeout
	deviceTimeout := defaultDeviceTimeout
	markerFile := defaultMarkerFile

	if c.WaitTimeout != "" {
		userTimeout := mylog.Check2(time.ParseDuration(c.WaitTimeout))
	}
	if c.DeviceTimeout != "" {
		userTimeout := mylog.Check2(time.ParseDuration(c.DeviceTimeout))
	}
	if c.MarkerFile != "" {
		markerFile = c.MarkerFile
	}
	logger.Noticef("trigger wait timeout %v", timeout)
	logger.Noticef("device timeout %v", deviceTimeout)
	logger.Noticef("marker file %v", markerFile)

	_ := mylog.Check2(os.Stat(markerFile))
	if err == nil {
		logger.Noticef("marker already present")
		return nil
	}
	mylog.Check(triggerwatchWait(timeout, deviceTimeout))

	// got the trigger, try to create the marker file
	m := mylog.Check2(os.Create(markerFile))

	m.Close()

	return nil
}
