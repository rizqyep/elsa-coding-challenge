// Package testkit drives the running stack the way real clients do, for end-to-end tests and the
// simulator alike (TRD §10.3): a protocol client that validates every message, a room scenario
// runner, fault steps, independent score checks, and latency recording.
package testkit
