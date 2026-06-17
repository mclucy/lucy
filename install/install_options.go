package install

type InstallOptions struct {
	WithOptional bool
	Force        bool
}

func DefaultOptions() InstallOptions {
	return InstallOptions{}
}
