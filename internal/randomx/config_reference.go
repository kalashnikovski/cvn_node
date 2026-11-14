//go:build randomx_reference

package randomx

// The two settings as published by the RandomX project. This file is only
// built with `-tags randomx_reference`, which exists so that the engine in
// this directory can be checked against RandomX's official test values:
//
//	go test -tags randomx_reference ./internal/randomx/
//
// CVN itself never runs with these values; see config_cvn.go.

// Argon2d salt.
const RANDOMX_ARGON_SALT = "RandomX\x03"

// Dataset extra size. Must be divisible by 64.
const RANDOMX_DATASET_EXTRA_SIZE = 33554368
