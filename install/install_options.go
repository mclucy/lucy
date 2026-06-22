package install

type InstallOptions struct {
	WithOptional bool
	Force        bool
	Journal      Journal
}

func DefaultOptions() InstallOptions {
	return InstallOptions{}
}

func (o InstallOptions) withDefaults() InstallOptions {
	if o.Journal == nil {
		o.Journal = logJournal{}
	}
	return o
}
