// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package aspects

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/strutil"
)

type ErrNotFound struct {
	Err string
}

func (e *ErrNotFound) Error() string {
	return e.Err
}

func (e *ErrNotFound) Is(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}

var validAccessValues = []string{"read", "write", "read-write"}

// DataBag controls access to a storage the data that the aspects access.
type DataBag interface {
	Get(path string, value interface{}) error
	Set(path string, value interface{}) error
	Data() ([]byte, error)
}

// Schema takes in data from the DataBag and validates that it's valid and could
// be committed.
type Schema interface {
	Validate(data []byte) error
}

// Directory holds a series of related aspects.
type Directory struct {
	Name    string
	dataBag DataBag
	schema  Schema
	aspects map[string]*Aspect
}

// NewAspectDirectory returns a new aspect directory for the following aspects
// and access patterns.
func NewAspectDirectory(name string, aspects map[string]interface{}, dataBag DataBag, schema Schema) (*Directory, error) {
	if len(aspects) == 0 {
		return nil, errors.New(`cannot create aspects directory: no aspects in map`)
	}

	aspectDir := Directory{
		Name:    name,
		dataBag: dataBag,
		schema:  schema,
		aspects: make(map[string]*Aspect, len(aspects)),
	}

	for name, v := range aspects {
		aspectPatterns, ok := v.([]map[string]string)
		if !ok {
			return nil, errors.New("cannot create aspect: access patterns should be a list of maps")
		} else if len(aspectPatterns) == 0 {
			return nil, errors.New("cannot create aspect without access patterns")
		}

		aspect := &Aspect{
			Name:           name,
			accessPatterns: make([]*accessPattern, 0, len(aspectPatterns)),
			directory:      aspectDir,
		}

		for _, aspectPattern := range aspectPatterns {
			name, ok := aspectPattern["name"]
			if !ok || name == "" {
				return nil, errors.New(`cannot create aspect pattern without a "name"`)
			}

			path, ok := aspectPattern["path"]
			if !ok || path == "" {
				return nil, errors.New(`cannot create aspect pattern without a "path"`)
			}

			access := aspectPattern["access"]
			if access != "" && strutil.ListContains(validAccessValues, access) {
				return nil, fmt.Errorf("cannot create aspect pattern: expected \"access\" to be one of %s instead of %q",
					strutil.Quoted(validAccessValues), access)
			}

			aspect.accessPatterns = append(aspect.accessPatterns, &accessPattern{
				name:   name,
				path:   path,
				access: access,
			})
		}

		aspectDir.aspects[name] = aspect
	}

	return &aspectDir, nil
}

// Aspect return an aspect from the aspect directory.
func (d *Directory) Aspect(aspect string) *Aspect {
	return d.aspects[aspect]
}

// Aspect is a group of access patterns under a directory.
type Aspect struct {
	Name           string
	accessPatterns []*accessPattern
	directory      Directory
}

func (a *Aspect) Set(name string, value interface{}) error {
	// TODO: add access control; name validation
	for _, p := range a.accessPatterns {
		if p.name == name {
			if err := a.directory.dataBag.Set(p.path, value); err != nil {
				return err
			}

			data, err := a.directory.dataBag.Data()
			if err != nil {
				return err
			}

			return a.directory.schema.Validate(data)
		}
	}

	return &ErrNotFound{fmt.Sprintf("cannot set name %q in aspect %q: access pattern not found", name, a.Name)}
}

func (a *Aspect) Get(name string, value interface{}) error {
	// TODO: add access control; name validation
	for _, p := range a.accessPatterns {
		if p.name == name {
			return a.directory.dataBag.Get(p.path, value)
		}
	}

	return &ErrNotFound{fmt.Sprintf("cannot get name %q in aspect %q: access pattern not found", name, a.Name)}
}

// accessPattern is an aspect accessPattern that holds information about a accessPattern.
type accessPattern struct {
	name   string
	path   string
	access string
}

// JSONDataBag is a simple DataBag implementation that keeps JSON in-memory.
type JSONDataBag map[string]json.RawMessage

func NewJSONDataBag() JSONDataBag {
	storage := make(map[string]json.RawMessage)
	return storage
}

func (s JSONDataBag) Get(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	err := get(subKeys, s, value)
	if uErr, ok := err.(*json.UnmarshalTypeError); ok {
		return fmt.Errorf("cannot read %q into variable of type \"%T\" because it maps to JSON %s", path, value, uErr.Value)
	}

	return err
}

func (s JSONDataBag) Set(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	_, err := set(subKeys, s, value)
	return err
}

func (s JSONDataBag) Data() ([]byte, error) {
	return json.Marshal(s)
}

type JSONSchema struct{}

func NewJSONSchema() *JSONSchema {
	return &JSONSchema{}
}

func (s *JSONSchema) Validate(jsonData []byte) error {
	// the top-level is always an object
	var data map[string]json.RawMessage
	return json.Unmarshal(jsonData, &data)
}

func get(subKeys []string, node map[string]json.RawMessage, result interface{}) error {
	key := subKeys[0]
	rawLevel, ok := node[key]
	if !ok {
		return &ErrNotFound{"key not found"}
	}

	if len(subKeys) == 1 {
		return json.Unmarshal(rawLevel, result)
	}

	var level map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level); err != nil {
		return err
	}

	return get(subKeys[1:], level, result)
}

func set(subKeys []string, node map[string]json.RawMessage, value interface{}) (json.RawMessage, error) {
	key := subKeys[0]

	if len(subKeys) == 1 {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		node[key] = data
		newData, err := json.Marshal(node)
		if err != nil {
			return nil, err
		}

		return newData, nil
	}

	rawLevel, ok := node[key]
	if !ok {
		rawLevel = []byte("{}")
	}

	var level map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(rawLevel), &level); err != nil {
		return nil, err
	}

	rawLevel, err := set(subKeys[1:], level, value)
	if err != nil {
		return nil, err
	}

	node[key] = rawLevel
	return json.Marshal(node)
}
