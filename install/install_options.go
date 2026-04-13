package install

type Options struct {
	WithOptional bool
}

func DefaultOptions() Options {
	return Options{}
}
