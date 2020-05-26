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
		if _, err := parser.AddCommand("recovery-chooser-trigger", short, long, &cmdRecoveryChooserTrigger{}); err != nil {
			panic(err)
		}
	})
}

var (
	triggerwatchWait = triggerwatch.Wait

	// default trigger wait timeout
	defaultTimeout = 10 * time.Second

	// default marker file location
	defaultMarkerFile = "/run/snapd-recovery-chooser-triggered"
)

type cmdRecoveryChooserTrigger struct {
	MarkerFile  string `long:"marker-file" value-name:"filename" description:"trigger marker file location"`
	WaitTimeout string `long:"wait-timeout" value-name:"duration" description:"trigger wait timeout"`
}

func (c *cmdRecoveryChooserTrigger) Execute(args []string) error {
	// TODO:UC20: check in the gadget if there is a hook or some binary we
	// should run for trigger detection. This will require some design work
	// and also thinking if/how such a hook can be confined.

	timeout := defaultTimeout
	markerFile := defaultMarkerFile

	if c.WaitTimeout != "" {
		userTimeout, err := time.ParseDuration(c.WaitTimeout)
		if err != nil {
			logger.Noticef("cannot parse duration %q, using default", c.WaitTimeout)
		} else {
			timeout = userTimeout
		}
	}
	if c.MarkerFile != "" {
		markerFile = c.MarkerFile
	}
	logger.Noticef("trigger wait timeout %v", timeout)
	logger.Noticef("marker file %v", markerFile)

	_, err := os.Stat(markerFile)
	if err == nil {
		logger.Noticef("marker already present")
		return nil
	}

	err = triggerwatchWait(timeout)
	if err != nil {
		switch err {
		case triggerwatch.ErrTriggerNotDetected:
			logger.Noticef("trigger not detected")
			return nil
		case triggerwatch.ErrNoMatchingInputDevices:
			logger.Noticef("no matching input devices")
			return nil
		default:
			return err
		}
	}

	// got the trigger, try to create the marker file
	m, err := os.Create(markerFile)
	if err != nil {
		return fmt.Errorf("cannot create the marker file: %q", err)
	}
	m.Close()

	return nil
}
