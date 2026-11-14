//go:build !randomx_reference

package randomx

// The two settings that make CVN's proof of work its own. Everything else in
// this directory is RandomX as published; see README_CVN.md.
//
// With these values a hash computed for CVN differs from the hash the same
// input gives on any other RandomX network, so mining software and prepared
// memory built for another coin do not produce valid CVN work. The memory
// sizes and the program generator, which are what keep graphics cards and
// custom chips from gaining an edge, are untouched.

// Argon2d salt (standard RandomX: "RandomX\x03").
const RANDOMX_ARGON_SALT = "RandomCVN\x01"

// Dataset extra size. Must be divisible by 64 (standard RandomX: 33554368).
const RANDOMX_DATASET_EXTRA_SIZE = 33554368 + 64*2026
