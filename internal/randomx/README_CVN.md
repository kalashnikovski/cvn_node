# RandomX engine used by CVN

This directory is a copy of the Go implementation of RandomX from
`git.gammaspectra.live/P2Pool/go-randomx` version 4.7.0 (BSD 3-clause; see
`LICENSE`), kept inside the repository so that the proof-of-work engine cannot
change or disappear underneath the project.

Changes from the original:

- Import paths point at this directory.
- Test files and the code generator were not copied.
- The two settings that identify a RandomX network (the Argon2 salt and the
  extra dataset size) moved from `config.go` into `config_cvn.go`, with CVN's
  own values. `config_reference.go` holds the published RandomX values.

Nothing else was changed. To confirm that, the engine can be built with the
published values and checked against RandomX's official test values:

    go test -tags randomx_reference ./internal/randomx/

Without that tag the tests check CVN's own values, and that the low-memory
(verifying) and full-memory (mining) modes agree.
