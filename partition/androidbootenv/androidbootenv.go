// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
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
	buf, err := ioutil.ReadFile(a.path)
	if err != nil {
		return err
	}

	rawEnv := bytes.Split(buf, []byte("\n"))
	for _, env := range rawEnv {
		l := bytes.SplitN(env, []byte("="), 2)
		// be liberal in what you accept
		if len(l) < 2 {
			continue
		}
		k := string(l[0])
		v := string(l[1])
		a.env[k] = v
	}

	return nil
}

func (a *Env) Save() error {
	w := bytes.NewBuffer(nil)

	for k, v := range a.env {
		if _, err := fmt.Fprintf(w, "%s=%s\n", k, v); err != nil {
			return err
		}
	}

	f, err := os.Create(a.path)
	if err != nil {
		return err
	}
	if _, err := f.Write(w.Bytes()); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}

	return f.Close()
}
