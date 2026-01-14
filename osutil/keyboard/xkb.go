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
	"github.com/snapcore/snapd/strutil"
)

type XKBConfig struct {
	Model    string
	Layouts  []string
	Variants []string
	Options  []string
}

// Validates that this is a valid XKB config that can be parsed
// by Plymouth.
func (c *XKBConfig) validate() error {
	if strings.Contains(c.Model, ",") {
		return fmt.Errorf("model cannot contain ',': found %q", c.Model)
	}
	if len(c.Variants) != 0 && len(c.Variants) != len(c.Layouts) {
		return fmt.Errorf("layouts and variants do not have the same length")
	}
	// XXX: Should we check that the model and layouts are always set?
	return nil
}

// KernelCommandLineFragment returns a simplified XKB configuration kernel
// cmdline fragment that can be parsed by plymouth-set-keymap.service.
//
// XKB config can each have multiple comma separated values
// but for early boot this is simplified and only the first
// value is considered to be able to compactly join the config
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
// fragment:
// snapd.xkb="us,pc105,,grp:alt_shift_toggle,terminate:ctrl_alt_bksp"
//
// Note that order is important and plymouth-set-keymap.service
// must match this ordering when parsing: layout,model,variant,option(s).
func (c *XKBConfig) KernelCommandLineFragment() string {
	var layout, variant string
	if len(c.Layouts) > 0 {
		layout = c.Layouts[0]
	}
	if len(c.Variants) > 0 {
		variant = c.Variants[0]
	}
	opts := strings.Join(c.Options, ",")

	simplified := fmt.Sprintf("%s,%s,%s,%s", layout, c.Model, variant, opts)
	return fmt.Sprintf("snapd.xkb=%q", simplified)
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
		Model: vals["X11Model"], // Only one model can be specified.
	}
	if vals["X11Layout"] != "" {
		config.Layouts = strings.Split(vals["X11Layout"], ",")
	}
	if vals["X11Variant"] != "" {
		config.Variants = strings.Split(vals["X11Variant"], ",")
	}
	if vals["X11Options"] != "" {
		config.Options = strings.Split(vals["X11Options"], ",")
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

func (w *XKBConfigListener) Close() {
	w.done()
	if err := w.iw.Close(); err != nil {
		logger.Noticef("cannot close inotify watcher: %v", err)
	}
}

// NewXKBConfigListener returns a XKBConfigListener that listens
// for XKB configuration changes and calls cb(XKBConfig) if a
// potential configuration change is detected.
func NewXKBConfigListener(ctx context.Context, cb func(config *XKBConfig)) (*XKBConfigListener, error) {
	iw, err := inotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// We care about IN_MOVED_TO as well to detect configuration updates that
	// happen atomically through replacement i.e. rename, renameat, renameat2.
	etcDir := filepath.Join(dirs.GlobalRootDir, "/etc")
	if err := iw.AddWatch(etcDir, inotify.InCloseWrite|inotify.InMovedTo); err != nil {
		return nil, err
	}
	etcDefaultDir := filepath.Join(dirs.GlobalRootDir, "/etc/default")
	if err := iw.AddWatch(etcDefaultDir, inotify.InCloseWrite|inotify.InMovedTo); err != nil {
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
	var kbConfigFiles = []string{
		filepath.Join(dirs.GlobalRootDir, "/etc/default/keyboard"),
		filepath.Join(dirs.GlobalRootDir, "/etc/vconsole.conf"),
	}
	for {
		select {
		case e := <-w.iw.Event:
			if !strutil.ListContains(kbConfigFiles, e.Name) {
				continue
			}
			// Note: Reading the XKB configuration straight after
			// the inotify event is fine because up until systemd
			// v259 it always reads the properties from the relevant
			// files, and since inotify is setup to only listen for
			// IN_CLOSE_WRITE and IN_MOVED_TO events so the content
			// should have been already written and the files closed.
			//
			// Keeping this in mind, There are no guarantees that
			// systemd's behavior stays the same, so the listener
			// callback notifications should always be considered
			// as best-effort only with no guarantees of always up
			// to date XKB configuration values.
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
