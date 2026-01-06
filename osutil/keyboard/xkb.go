// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package keyboard

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/inotify"
)

type XKBConfig struct {
	Model    string
	Layouts  []string
	Variants []string
	Options  []string
}

func (c *XKBConfig) validate() error {
	if strings.Contains(c.Model, ",") {
		return fmt.Errorf("model cannot contain ',': found %q", c.Model)
	}
	if len(c.Layouts) != len(c.Variants) {
		return fmt.Errorf("layouts and variants do not have the same length")
	}
	return nil
}

// KernelCommandLineValue returns a simplified XKB configuration
// value that can be parsed by plymouth-set-keymap.service.
//
// XKB config can each have multiple comma separated values
// but for early boot this is simplified and only the first
// value is considered to able to compactly join the config
// fields using a comma-separator except for XKBOPTIONS=
// which is appended to the end as is.
//
// A typical XKB configuration looks something like this:
//
//	-------------------------
//	XKBMODEL="pc105"
//	XKBLAYOUT="us,cz,de"
//	XKBVARIANT=",bksl,"
//	XKBOPTIONS="grp:alt_shift_toggle,terminate:ctrl_alt_bksp"
//	-------------------------
//
// This example would be simplified to the following kernel cmdline
// argument:
// snapd.xkb="us,pc105,,grp:alt_shift_toggle,terminate:ctrl_alt_bksp"
//
// Note that order is important and plymouth-set-keymap.service
// must match this ordering when parsing: layout,model,variant,option(s).
func (c *XKBConfig) KernelCommandLineValue() string {
	var layout, variant string
	if len(c.Layouts) > 0 {
		layout = c.Layouts[0]
	}
	if len(c.Variants) > 0 {
		variant = c.Variants[0]
	}
	opts := strings.Join(c.Options, ",")

	return fmt.Sprintf("%s,%s,%s,%s", layout, c.Model, variant, opts)
}

func CurrentXKBConfig() (*XKBConfig, error) {
	conn, err := dbusutil.SystemBus()
	if err != nil {
		return nil, err
	}

	obj := conn.Object("org.freedesktop.locale1", "/org/freedesktop/locale1")

	dbusVals := make(map[string]dbus.Variant)
	err = obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.locale1").Store(&dbusVals)
	if err != nil {
		return nil, err
	}

	properties := []string{"X11Layout", "X11Model", "X11Variant", "X11Options"}
	vals := make(map[string]string, len(properties))
	for _, property := range properties {
		val, ok := dbusVals[property].Value().(string)
		if !ok {
			return nil, fmt.Errorf("internal error: expected type string for %q, found %T", property, dbusVals[property].Value())
		}
		vals[property] = val
	}

	// XXX: Fallback to parsing /etc/default/keyboard and /etc/vconsole.conf
	// if we fail to obtain values over dbus?

	config := &XKBConfig{
		Model:    vals["X11Model"], // Only one model can be specified.
		Layouts:  strings.Split(vals["X11Layout"], ","),
		Variants: strings.Split(vals["X11Variant"], ","),
		Options:  strings.Split(vals["X11Options"], ","),
	}
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("cannot parse XKB configuration: %v", err)
	}

	return config, nil
}

type XKBConfigListener struct {
	iw *inotify.Watcher

	ctx  context.Context
	done context.CancelFunc

	cb func(config *XKBConfig)
}

func (w *XKBConfigListener) Close() error {
	w.done()
	return w.iw.Close()
}

// NewXKBConfigListener returns a XKBConfigListener that listens
// for XKB configuration changes and calls cb(XKBConfig) if a
// potential configuration change is detected.
func NewXKBConfigListener(ctx context.Context, cb func(config *XKBConfig)) (*XKBConfigListener, error) {
	iw, err := inotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// XXX: Should we instead only error out if both files are not
	// present? If both files don't exist, Should we fallback to a
	// timer based trigger instead of erroring out for robustness?
	defaultKeyboardConf := filepath.Join(dirs.GlobalRootDir, "/etc/default/keyboard")
	if err := iw.AddWatch(defaultKeyboardConf, inotify.InCloseWrite); err != nil {
		return nil, err
	}
	vconsoleConf := filepath.Join(dirs.GlobalRootDir, "/etc/vconsole.conf")
	if err := iw.AddWatch(vconsoleConf, inotify.InCloseWrite); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	watcher := &XKBConfigListener{
		iw:   iw,
		ctx:  ctx,
		done: cancel,
		cb:   cb,
	}
	go watcher.loop()
	return watcher, nil
}

func (w *XKBConfigListener) loop() {
	for {
		select {
		case <-w.iw.Event:
			config, err := CurrentXKBConfig()
			if err != nil {
				logger.Noticef("cannot obtain current XKB configuration: %v", err)
				continue
			}
			w.cb(config)
		case <-w.ctx.Done():
			w.Close()
			return
		}
	}
}
