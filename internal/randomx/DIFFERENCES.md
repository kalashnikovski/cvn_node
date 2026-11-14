# The RandomX engine compared with its original

Written by `audit/compare_randomx.py`. Do not edit by hand; run the script again.

The original is `git.gammaspectra.live/P2Pool/go-randomx/v4` version 4.7.0, as fetched by Go and checked against Go's public checksum database (`h1:bgsnAaOAyBMIU6hmg7JibsoTeSt9C6PbtEHn1fMvPtg=`).
Files are compared byte for byte, allowing only for line endings and for the import path.

| | Files |
|---|---|
| Identical to the original | 71 |
| Changed | 3 |
| In the original and not copied | 14 |
| Added by CVN | 6 |

## Changed files, with every changed line

### `config.go`

```diff
--- original/config.go
+++ cvn/config.go
@@ -45,7 +45,5 @@
 const RANDOMX_ARGON_LANES = 1
 
-// Argon2d salt
-const RANDOMX_ARGON_SALT = "RandomX\x03"
-const ArgonSaltSize uint32 = 8 //sizeof("" RANDOMX_ARGON_SALT) - 1
+// Argon2d salt: see config_cvn.go (and config_reference.go).
 
 // Number of random Cache accesses per Dataset item. Minimum is 2.
@@ -58,6 +56,5 @@
 const RANDOMX_DATASET_BASE_SIZE = 2147483648
 
-// Dataset extra size. Must be divisible by 64.
-const RANDOMX_DATASET_EXTRA_SIZE = 33554368
+// Dataset extra size: see config_cvn.go (and config_reference.go).
 
 // Number of instructions in a RandomX program. Must be divisible by 8.
```

### `internal/memory/large_other.go`

```diff
--- original/internal/memory/large_other.go
+++ cvn/internal/memory/large_other.go
@@ -1,3 +1,3 @@
-//go:build openbsd || netbsd || dragonfly || (darwin && !amd64) || ios || !unix || purego
+//go:build openbsd || netbsd || dragonfly || (darwin && !amd64) || ios || (!unix && !windows) || purego
 
 package memory
```

### `vm.go`

```diff
--- original/vm.go
+++ cvn/vm.go
@@ -63,4 +63,8 @@
 	Program    ByteCode
 	JITProgram VMProgramFunc
+
+	// largePages records that this state was allocated in large pages
+	// (CVN addition; see README_CVN.md).
+	largePages bool
 }
 
@@ -71,11 +75,26 @@
 		}
 	}
+	if s.largePages {
+		return memory.Free(largePageAllocator, s)
+	}
 	return memory.Free(cacheLineAlignedAllocator, s)
 }
 
 func NewVMState(vm *VM) (*VMState, error) {
-	s, err := memory.Allocate[VMState](cacheLineAlignedAllocator)
-	if err != nil {
-		return nil, err
+	// CVN addition: with RANDOMX_FLAG_LARGE_PAGES the state, which is mostly
+	// the 2 MiB scratchpad every hash works in, is placed in large pages when
+	// the system grants them, and in ordinary memory otherwise.
+	var s *VMState
+	var err error
+	if vm.flags.Has(RANDOMX_FLAG_LARGE_PAGES) && largePageAllocator != nil {
+		if s, err = memory.Allocate[VMState](largePageAllocator); err == nil {
+			s.largePages = true
+		}
+	}
+	if s == nil {
+		s, err = memory.Allocate[VMState](cacheLineAlignedAllocator)
+		if err != nil {
+			return nil, err
+		}
 	}
 
```

## Added by CVN

- `README_CVN.md`
- `config_cvn.go`
- `config_reference.go`
- `cvn_test.go`
- `internal/memory/large_windows.go`
- `reference_test.go`

## In the original and not copied

- `.drone.jsonnet`
- `.drone.yml`
- `README.md`
- `cache_test.go`
- `commitment_test.go`
- `go.mod`
- `go.sum`
- `internal/aes/aes_test.go`
- `internal/generate-inlinevm/main.go`
- `math_test.go`
- `randomx_generic_test.go`
- `randomx_test.go`
- `randomx_tinygo_test.go`
- `superscalar_test.go`

## Identical to the original

- `LICENSE`
- `cache.go`
- `commitment.go`
- `dataset.go`
- `exec.go`
- `flags.go`
- `flags_generic.go`
- `flags_purego.go`
- `internal/aes/const.go`
- `internal/aes/hard.go`
- `internal/aes/hard_amd64.go`
- `internal/aes/hard_arm64.go`
- `internal/aes/hard_arm64_darwin.go`
- `internal/aes/hard_generic.go`
- `internal/aes/hash.go`
- `internal/aes/impl.go`
- `internal/aes/round.go`
- `internal/aes/soft.go`
- `internal/argon2/argon2.go`
- `internal/asm/aes_amd64.go`
- `internal/asm/aes_amd64.s`
- `internal/asm/aes_arm64.go`
- `internal/asm/aes_arm64.s`
- `internal/asm/aes_noasm.go`
- `internal/asm/cpuid_amd64.go`
- `internal/asm/cpuid_amd64.s`
- `internal/asm/round.go`
- `internal/asm/round_386.go`
- `internal/asm/round_386.s`
- `internal/asm/round_amd64.go`
- `internal/asm/round_amd64.s`
- `internal/asm/round_arm.go`
- `internal/asm/round_arm.s`
- `internal/asm/round_arm64.go`
- `internal/asm/round_arm64.s`
- `internal/asm/round_noasm.go`
- `internal/blake2/generator.go`
- `internal/keys/keys.go`
- `internal/memory/aligned.go`
- `internal/memory/alloc.go`
- `internal/memory/large.go`
- `internal/memory/large_darwin.go`
- `internal/memory/large_freebsd.go`
- `internal/memory/large_unix.go`
- `internal/memory/pages_linux.go`
- `internal/memory/pages_netbsd.go`
- `internal/memory/pages_other.go`
- `internal/memory/pages_unix.go`
- `internal/memory/pages_windows.go`
- `jit_amd64.go`
- `jit_generic.go`
- `math.go`
- `randomx.go`
- `register.go`
- `runtime.go`
- `superscalar.go`
- `superscalar_exec.go`
- `superscalar_inlinevm.go`
- `superscalar_instruction.go`
- `superscalar_jit_amd64.go`
- `superscalar_jit_amd64.s`
- `superscalar_noasm.go`
- `vm_bytecode.go`
- `vm_bytecode_jit_amd64.go`
- `vm_bytecode_jit_amd64.s`
- `vm_bytecode_jit_generic.go`
- `vm_bytecode_native.go`
- `vm_bytecode_purego.go`
- `vm_instruction.go`
- `vm_instruction_native.go`
- `vm_instruction_purego.go`
