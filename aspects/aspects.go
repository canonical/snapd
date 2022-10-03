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

// Directory holds a series of related aspects.
type Directory struct {
	Name    string
	dataBag DataBag
	aspects map[string]*Aspect
}

// NewAspectDirectory returns a new aspect directory for the following aspects
// and access patterns.
func NewAspectDirectory(name string, aspects map[string]interface{}, dataBag DataBag) (*Directory, error) {
	if len(aspects) == 0 {
		return nil, errors.New(`cannot create aspects directory: no aspects in map`)
	}

	aspectDir := Directory{
		Name:    name,
		dataBag: dataBag,
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
	// TODO: add access control; name validation

	for _, p := range a.accessPatterns {
		if p.name == name {
			return a.directory.dataBag.Set(p.path, value)
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

// JSONStorage is a simple Schema implementation that keeps JSON in-memory.
type JSONStorage map[string]json.RawMessage

func NewStorage() JSONStorage {
	storage := make(map[string]json.RawMessage)
	return storage
}

func (s JSONStorage) Validate(string) error { return nil }

func (s JSONStorage) Get(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	return get(subKeys, s, value)
}

func (s JSONStorage) Set(path string, value interface{}) error {
	subKeys := strings.Split(path, ".")
	_, err := set(subKeys, s, value)
	return err
}

func (s JSONStorage) Data() ([]byte, error) {
	return json.Marshal(s)
}

func get(subKeys []string, root map[string]json.RawMessage, result interface{}) error {
	key := subKeys[0]
	value, ok := root[key]
	if !ok {
		return &ErrNotFound{"key not found"}
	}

	if len(subKeys) == 1 {
		return json.Unmarshal(value, result)
	}

	var nextLevel map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(value), &nextLevel); err != nil {
		return err
	}

	return get(subKeys[1:], nextLevel, result)
}

func set(subKeys []string, root map[string]json.RawMessage, result interface{}) (json.RawMessage, error) {
	key := subKeys[0]

	if len(subKeys) == 1 {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}

		root[key] = data
		newData, err := json.Marshal(root)
		if err != nil {
			return nil, err
		}

		return newData, nil
	}

	value, ok := root[key]
	if !ok {
		value = []byte("{}")
	}

	var nextLevel map[string]json.RawMessage
	if err := jsonutil.DecodeWithNumber(bytes.NewReader(value), &nextLevel); err != nil {
		return nil, err
	}

	value, err := set(subKeys[1:], nextLevel, result)
	if err != nil {
		return nil, err
	}

	root[key] = value
	return json.Marshal(root)
}
