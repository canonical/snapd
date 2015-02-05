package snappy

// var instead of const to make it possible to override in the tests
var (
	snapBaseDir = "/apps"
	snapOemDir  = "/oem"
	snapDataDir = "/var/lib/apps"
)

type SnapType string

const (
	SnapTypeApp       SnapType = "app"
	SnapTypeCore      SnapType = "core"
	SnapTypeFramework SnapType = "framework"
	SnapTypeOem       SnapType = "oem"
)

// Representation of a snappy part
type Part interface {

	// query
	Name() string
	Version() string
	Description() string

	Hash() string
	IsActive() bool
	IsInstalled() bool

	// app, framework, core
	Type() SnapType

	InstalledSize() int
	DownloadSize() int

	// Action
	Install(pb ProgressMeter) error
	Uninstall() error
	Config(configuration []byte) error
}

type Repository interface {

	// query
	Description() string

	// action
	Search(terms string) ([]Part, error)
	Details(snappName string) ([]Part, error)

	Updates() ([]Part, error)
	Installed() ([]Part, error)
}

type MetaRepository struct {
	all []Repository
}

func NewMetaRepository() *MetaRepository {
	// FIXME: make this a configuration file

	m := new(MetaRepository)
	m.all = []Repository{
		NewSystemImageRepository(),
		NewUbuntuStoreSnapRepository()}
	// these may fail if there is no such directory
	repo := NewLocalSnapRepository(snapBaseDir)
	if repo != nil {
		m.all = append(m.all, repo)
	}
	repo = NewLocalSnapRepository(snapOemDir)
	if repo != nil {
		m.all = append(m.all, repo)
	}

	return m
}

func (m *MetaRepository) Installed() (parts []Part, err error) {
	for _, r := range m.all {
		installed, err := r.Installed()
		if err != nil {
			return parts, err
		}
		parts = append(parts, installed...)
	}

	return parts, err
}

func (m *MetaRepository) Updates() (parts []Part, err error) {
	for _, r := range m.all {
		updates, err := r.Updates()
		if err != nil {
			return parts, err
		}
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

func (m *MetaRepository) Details(snapyName string) (parts []Part, err error) {
	for _, r := range m.all {
		results, err := r.Details(snapyName)
		if err != nil {
			return parts, err
		}
		parts = append(parts, results...)
	}

	return parts, err
}

func InstalledSnapsByType(snapTs ...SnapType) (res []Part, err error) {
	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return nil, err
	}

	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		for i := range snapTs {
			if part.Type() == snapTs[i] {
				res = append(res, part)
			}
		}
	}
	return
}

var InstalledSnapNamesByType = func(snapTs ...SnapType) (res []string, err error) {
	installed, err := InstalledSnapsByType(snapTs...)
	for _, part := range installed {
		res = append(res, part.Name())
	}
	return
}

func InstalledSnapByName(needle string) Part {
	m := NewMetaRepository()
	installed, err := m.Installed()
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

func FindPartByName(needle string, haystack []Part) *Part {
	for _, part := range haystack {
		if part.Name() == needle {
			return &part
		}
	}
	return nil
}
