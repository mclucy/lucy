// Package discovery observes Minecraft server installations in one directory.
//
// Discovery is local-only. It scans a server directory through a deterministic
// static registry of private directory recognizers, reconciles their output
// into installations, verifies the optional local ownership receipt, and makes
// one explicit read-only runtime selection. It never contacts upstream
// services and never writes workspace state.
//
// All observed members of an installation live in one Files collection with
// typed roles. An entry point is a filter over that collection, not a second
// parallel list. Result carries no open filesystem or ZIP handles: the scan
// cache is closed before Discover returns.
package discovery
