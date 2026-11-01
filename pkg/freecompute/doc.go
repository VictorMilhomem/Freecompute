// Package freecompute provides a public v0 Go SDK for verifiable volunteer
// computing over Freechains, Kubo IPFS, and rootless Podman.
//
// Status: v0 (experimental, API subject to change).
//
// A Node participates on a Freechains chain as a job Submitter (Node.Submit),
// a container Worker (Node.Serve), or both concurrently. Inputs and outputs are
// content-addressed directories in IPFS, and protocol phases advance
// deterministically by chain consensus positions using commit-reveal and
// minimum quorum.
package freecompute
