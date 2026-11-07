// Package upstream defines the core upstream abstraction layer.
//
// catalog.Provider identifies the execution layer that fetches metadata or
// artifacts; catalog.Registry identifies a fully functional package repository
// and is executed by exactly one provider. Provider capabilities implement the
// small interfaces in this package; server-product operations consumed by
// routing remain concrete loader methods. Provider selection,
// unspecified-provider policy, and multi-provider execution strategies live
// outside this package in upstream/routing.
//
// Dependency inversion: this package defines the capability interfaces and
// normalized conversion contracts. Concrete providers implement those small
// interfaces and depend on these contracts, not the other way around.
// Callers execute capability interfaces through upstream/routing.
package upstream
