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

package grubenv

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
)

type Grubenv struct {
	env      map[string]string
	ordering []string

	path string
}

func NewGrubenv(path string) *Grubenv {
	return &Grubenv{
		env:  make(map[string]string),
		path: path,
	}
}

func (g *Grubenv) Getenv(name string) string {
	return g.env[name]
}

func (g *Grubenv) Setenv(key, value string) {
	for i, k := range g.ordering {
		if k == key {
			g.ordering = append(g.ordering[:i], g.ordering[i+1:]...)
		}
	}
	g.ordering = append(g.ordering, key)

	g.env[key] = value
}

func (g *Grubenv) Load() error {
	buf, err := ioutil.ReadFile(g.path)
	if err != nil {
		return err
	}
	if len(buf) != 1024 {
		return fmt.Errorf("grubenv %q must be exactly 1024 byte, got %d", g.path, len(buf))
	}
	if !bytes.HasPrefix(buf, []byte("# GRUB Environment Block\n")) {
		return fmt.Errorf("cannot find grubenv header in %q", g.path)
	}
	rawEnv := bytes.Split(buf, []byte("\n"))
	for _, env := range rawEnv[1:] {
		l := bytes.Split(env, []byte("="))
		// be liberal what you accept
		if len(l) < 2 {
			continue
		}
		k := string(l[0])
		v := string(l[1])
		g.env[k] = v
		g.ordering = append(g.ordering, k)
	}

	return nil
}

func (g *Grubenv) Save() error {
	w := bytes.NewBuffer(nil)
	w.Grow(1024)

	fmt.Fprintf(w, "# GRUB Environment Block\n")
	for _, k := range g.ordering {
		if _, err := fmt.Fprintf(w, "%s=%s\n", k, g.env[k]); err != nil {
			return err
		}
	}
	if w.Len() > 1024 {
		return fmt.Errorf("cannot write grubenv %q: bigger than 1024 bytes (%d)", w.Len())
	}
	// fill up with "#" at the envgit
	for i := w.Len(); i < 1024; i++ {
		w.Write([]byte("#"))
	}

	// write in place to avoid the file moving on disk
	// (thats what grubenv is also doing)
	f, err := os.Create(g.path)
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
