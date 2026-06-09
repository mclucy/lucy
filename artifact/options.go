package artifact

// Option configures Analyze behavior.
type Option func(*options)

type options struct {
	slugResolver SlugResolver
}

// WithSlugResolver injects a slug resolver that normalizes package names
// after detection. If not set, names are used as-is from the artifact metadata.
func WithSlugResolver(fn SlugResolver) Option {
	return func(o *options) {
		o.slugResolver = fn
	}
}
