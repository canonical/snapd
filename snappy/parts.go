package snappy

// Representation of a snappy part
type Part interface {

	// querry
	Name() string
	Version() string
	Description() string

	Hash() string
	IsActive() bool
	IsInstalled() bool

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
	GetUpdates() ([]Part, error)
	GetInstalled() ([]Part, error)
}

type MetaRepository struct {
	all []Repository
}

func NewMetaRepository() *MetaRepository {
	m := new(MetaRepository)
	m.all = []Repository{NewSystemImageRepository()}

	return m
}

func (m *MetaRepository) GetInstalled() (parts []Part, err error) {
	for _, r := range m.all {
		installed, err := r.GetInstalled()
		if err != nil {
			return []Part{}, err
		}
		// FIXME: python extend() anyone?
		for _, part := range installed {
			parts = append(parts, part)
		}
	}

	return parts, err
}
