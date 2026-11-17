//go:build !(amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x)

package node

// This file is built only on a platform whose whole numbers are 32 bits
// wide, and it does not compile there, on purpose.
//
// A block's difficulty is held as a whole number of the platform's natural
// size. Once the network's mining power passes about 48 million hashes a
// second, difficulty no longer fits in 32 bits; a node built for a 32-bit
// platform would then refuse every block the rest of the network accepts.
// Better not to build than to build something that will one day leave the
// network. (Audit round 5, finding 2.)
var _ = cvnNeedsA64BitPlatform
