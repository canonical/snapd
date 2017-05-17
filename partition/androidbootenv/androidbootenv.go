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

package androidbootenv

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type Env struct {
	env  map[string]string
	path string
}

func NewEnv(path string) *Env {
	return &Env{
		env:  make(map[string]string),
		path: path,
	}
}

func (a *Env) Get(name string) string {
	return a.env[name]
}

func (a *Env) Set(key, value string) {
	a.env[key] = value
}

func (a *Env) Load() error {
	file, err := os.Open(a.path)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		l := strings.SplitN(scanner.Text(), "=", 2)
		// be liberal in what you accept
		if len(l) < 2 {
			logger.Noticef("WARNING: bad value while parsing %v", a.path)
			continue
		}
		a.env[l[0]] = l[1]
	}

	return nil
}

func (a *Env) Save() error {
	var w bytes.Buffer

	for k, v := range a.env {
		if _, err := fmt.Fprintf(&w, "%s=%s\n", k, v); err != nil {
			return err
		}
	}

	return osutil.AtomicWriteFile(a.path, w.Bytes(), 0644, 0)
}
