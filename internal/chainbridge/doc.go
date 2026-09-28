// Package chainbridge implements the single point of contact actor between
// the volunteer-computing system and a Freechains chain.
//
// The bridge publishes Protobuf envelopes, polls the consensus order, detects
// DAG reorganizations, and emits an ordered, deduplicated stream of decoded
// envelopes to the rest of the system via an EventSink.
//
// Startup and Restart Behavior:
// On start, the bridge performs an immediate poll of the consensus order,
// followed by periodic poll and sync ticks. On supervisor restart, the bridge
// initializes with a fresh Tracker state and re-emits the consensus order from
// index 0. Consumers (Job and Task actors) must therefore be idempotent.
package chainbridge
