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

// Schema maps dotted paths to underlying storage mechanism.
type Schema interface {
	Get(path string, value interface{}) error
	Set(path string, value interface{}) error
	Validate(name string) error
}

// Directory holds a series of related aspects.
type Directory struct {
	Name    string
	schema  Schema
	aspects map[string]*Aspect
}

// NewAspectDirectory returns a new aspect directory for the following aspects
// and access patterns.
func NewAspectDirectory(name string, mapping map[string]interface{}, schema Schema) (*Directory, error) {
	a, ok := mapping["aspects"]
	if !ok {
		return nil, errors.New("cannot create aspects directory without aspects map")
	}

	aspects, ok := a.(map[string]interface{})
	if !ok {
		return nil, errors.New(`cannot create aspects directory: "aspects" key should map to an aspects map`)
	}

	aspectDir := Directory{
		Name:    name,
		schema:  schema,
		aspects: make(map[string]*Aspect, len(aspects)),
	}

	for name, v := range aspects {
		aspectViews, ok := v.([]map[string]string)
		if !ok {
			return nil, errors.New("cannot create aspect: access patterns should be a list of maps")
		} else if len(aspectViews) == 0 {
			return nil, errors.New("cannot create aspect without access patterns")
		}

		aspect := &Aspect{
			Name:           name,
			accessPatterns: make([]*accessPattern, 0, len(aspectViews)),
			directory:      aspectDir,
		}

		for _, aspectView := range aspectViews {
			name, ok := aspectView["name"]
			if !ok || name == "" {
				return nil, errors.New(`cannot create aspect view without a "name"`)
			}

			path, ok := aspectView["path"]
			if !ok || path == "" {
				return nil, errors.New(`cannot create aspect view without a "path"`)
			}

			access := aspectView["access"]
			if access != "" && strutil.ListContains(validAccessValues, access) {
				return nil, fmt.Errorf("cannot create aspect view: expected \"access\" to be one of %s instead of %q",
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
	// TODO: add access control

	for _, p := range a.accessPatterns {
		if p.name == name {
			return a.directory.schema.Set(p.path, value)
		}
	}

	return &ErrNotFound{fmt.Sprintf("cannot set name %q in aspect %q: access pattern not found", name, a.Name)}
}

func (a *Aspect) Get(name string, value interface{}) error {
	// TODO: add access control

	for _, p := range a.accessPatterns {
		if p.name == name {
			return a.directory.schema.Get(p.path, value)
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

// JSONStorage is a simple Schema implementation that keeps JSON in-memory.
type JSONStorage map[string][]byte

func NewStorage() JSONStorage {
	storage := make(map[string][]byte)
	storage["aspects"] = []byte("{}")
	return storage
}

func (s JSONStorage) Validate(string) error { return nil }

func (s JSONStorage) Get(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	return s.get(subKeys, s["aspects"], value)
}

func (s *JSONStorage) get(subKeys []string, root []byte, result interface{}) error {
	var curMap map[string][]byte
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(root), &curMap); err != nil {
		return err
	}

	key := subKeys[0]
	value, ok := curMap[key]
	if !ok {
		return &ErrNotFound{"key not found"}
	}

	if len(subKeys) == 1 {
		return json.Unmarshal(value, result)
	}

	return s.get(subKeys[1:], value, result)
}

func (s JSONStorage) Set(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	aspectsData, err := s.set(subKeys, s["aspects"], value)
	if err != nil {
		return err
	}

	s["aspects"] = aspectsData
	return nil
}

func (s *JSONStorage) set(subKeys []string, raw []byte, value interface{}) ([]byte, error) {
	if len(subKeys) == 1 {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		var curMap map[string][]byte
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(raw), &curMap); err != nil {
			return nil, err
		}

		curMap[subKeys[0]] = data
		newData, err := json.Marshal(curMap)
		if err != nil {
			return nil, err
		}

		return newData, nil
	}

	var curMap map[string][]byte
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(raw), &curMap); err != nil {
		return nil, err
	}

	nextLevel, ok := curMap[subKeys[0]]
	if !ok {
		nextLevel = []byte("{}")
	}

	nextLevel, err := s.set(subKeys[1:], nextLevel, value)
	if err != nil {
		return nil, err
	}

	curMap[subKeys[0]] = nextLevel
	return json.Marshal(curMap)
}
