package snappy

import (
	"strings"
)

const (
	snapBaseDir = "/apps"
	snapOemDir  = "/oem"
)

// Representation of a snappy part
type Part interface {

	// querry
	Name() string
	Version() string
	Description() string

	Hash() string
	IsActive() bool
	IsInstalled() bool

	// app, framework, core
	Type() string

	InstalledSize() int
	DownloadSize() int

	// Action
	Install() error
	Uninstall() error
	Config(configuration []byte) error
}

type Repository interface {

	// query
	Description() string

	// action
	Search(terms string) ([]Part, error)
	Details(snappName string) ([]Part, error)

	GetUpdates() ([]Part, error)
	GetInstalled() ([]Part, error)
}

type MetaRepository struct {
	all []Repository
}

func NewMetaRepository() *MetaRepository {
	// FIXME: make this a configuration file
	
	m := new(MetaRepository)
	m.all = []Repository{
		NewSystemImageRepository(),
		NewUbuntuStoreSnappRepository()}
	// these may fail if there is no such directory
	repo := NewLocalSnappRepository(snapBaseDir)
	if repo != nil {
		m.all = append(m.all, repo)
	}
	repo = NewLocalSnappRepository(snapOemDir)
	if repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

func (m *MetaRepository) GetInstalled() (parts []Part, err error) {
	for _, r := range m.all {
		installed, _ := r.GetInstalled()
		parts = append(parts, installed...)
	}

	return parts, err
}

func (m *MetaRepository) GetUpdates() (parts []Part, err error) {
	for _, r := range m.all {
		updates, _ := r.GetUpdates()
		parts = append(parts, updates...)
	}

	return parts, err
}

func (m *MetaRepository) Search(terms string) (parts []Part, err error) {
	for _, r := range m.all {
		results, err := r.Search(terms)
		if err != nil {
			return parts, err
		}
		parts = append(parts, results...)
	}

	return parts, err
}

func (m *MetaRepository) Details(snappyName string) (parts []Part, err error) {
	for _, r := range m.all {
		results, err := r.Details(snappyName)
		if err != nil {
			return parts, err
		}
		parts = append(parts, results...)
	}

	return parts, err
}

func GetInstalledSnappsByType(searchExp string) (res []Part, err error) {
	m := NewMetaRepository()
	installed, err := m.GetInstalled()
	if err != nil {
		return res, err
	}
	snappTypes := strings.Split(searchExp, ",")
	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		for _, snappType := range snappTypes {
			if part.Type() == snappType {
				res = append(res, part)
			}
		}
	}
	return
}

var GetInstalledSnappNamesByType = func(snappType string) (res []string, err error) {
	installed, err := GetInstalledSnappsByType(snappType)
	for _, part := range installed {
		res = append(res, part.Name())
	}
	return
}

func GetInstalledSnappByName(needle string) Part {
	m := NewMetaRepository()
	installed, err := m.GetInstalled()
	if err != nil {
		return nil
	}
	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		if part.Name() == needle {
			return part
		}
	}
	return nil
}

func findPartByName(needle string, haystack []Part) *Part {
	for _, part := range haystack {
		if part.Name() == needle {
			return &part
		}
	}
	return nil
}
