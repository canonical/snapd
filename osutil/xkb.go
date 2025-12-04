// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package osutil

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/inotify"
)

type XKBConfig struct {
	Models   []string
	Layouts  []string
	Variants []string
	Options  []string
}

func CurrentXKBConfig() (*XKBConfig, error) {
	conn, err := dbusutil.SystemBus()
	if err != nil {
		return nil, err
	}

	obj := conn.Object("org.freedesktop.locale1", "/org/freedesktop/locale1")
	properties := []string{"X11Layout", "X11Model", "X11Variant", "X11Options"}
	vals := make(map[string]string, 4)
	for _, property := range properties {
		dbusVal, err := obj.GetProperty(fmt.Sprintf("org.freedesktop.locale1.%s", property))
		if err != nil {
			return nil, err
		}

		val, ok := dbusVal.Value().(string)
		if !ok {
			return nil, fmt.Errorf("internal error: expected type string, found %T", dbusVal.Value())
		}
		vals[property] = val
	}

	// XXX: Fallback to parsing /etc/default/keyboard if we fail to obtain
	// values over dbus?

	config := &XKBConfig{
		Layouts:  strings.Split(vals["X11Layout"], ","),
		Models:   strings.Split(vals["X11Model"], ","),
		Variants: strings.Split(vals["X11Variant"], ","),
		Options:  strings.Split(vals["X11Options"], ","),
	}
	return config, nil
}

type XKBConfigListener struct {
	iw *inotify.Watcher

	ctx  context.Context
	done context.CancelFunc

	cb func()
}

func (w *XKBConfigListener) Close() error {
	w.done()
	return w.iw.Close()
}

// NewXKBConfigListener returns a XKBConfigListener that
// listens for XKB configuration changes and calls cb()
// if a configuration change is detected.
func NewXKBConfigListener(ctx context.Context, cb func()) (*XKBConfigListener, error) {
	iw, err := inotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	xkbConfigFile := filepath.Join(dirs.GlobalRootDir, "/etc/default/keyboard")
	if err := iw.AddWatch(xkbConfigFile, inotify.InCloseWrite); err != nil {
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
			w.cb()
		case <-w.ctx.Done():
			w.Close()
			return
		}
	}
}
