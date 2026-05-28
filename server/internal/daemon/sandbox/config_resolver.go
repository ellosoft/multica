package sandbox

// configStore abstracts the sqlc-backed lookups so the resolver is unit-testable.
type configStore interface {
	projectConfig(projectID string) (*Config, error)
	globalConfig() (*Config, error)
}

// Resolver implements project→global precedence.
type Resolver struct{ Store configStore }

func (r Resolver) Resolve(projectID string) (Config, error) {
	if pc, err := r.Store.projectConfig(projectID); err != nil {
		return Config{}, err
	} else if pc != nil {
		return *pc, nil
	}
	if gc, err := r.Store.globalConfig(); err != nil {
		return Config{}, err
	} else if gc != nil {
		return *gc, nil
	}
	return Config{Enabled: false}, nil
}
