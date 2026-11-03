// Package random is the kernel's source of random bytes (W14-S03):
// getrandom(2), /dev/random and /dev/urandom all read Kernel. It is a
// ChaCha8 generator (math/rand/v2) that boot seeds once, from RDRAND when
// the CPU has it, else from the TSC (arch/amd64.Seed).
//
// It must never be taken for a cryptographic source: a TSC seed can be
// guessed, nothing reseeds the generator, and writes do not mix in. It is
// enough for hash seeds, temporary names and games. Docs: POSIX_COVERAGE.md.
package random

import (
	"encoding/binary"
	"math/rand/v2"
	"sync"
)

// Source is safe for concurrent use; it is a devfs device as it stands.
type Source struct {
	mu sync.Mutex
	g  *rand.ChaCha8
}

func New(seed [32]byte) *Source { return &Source{g: rand.NewChaCha8(seed)} }

// Kernel starts from the Go runtime's own seed until boot calls Seed.
var Kernel = New(runtimeSeed())

func runtimeSeed() (seed [32]byte) {
	for i := 0; i < len(seed); i += 8 {
		binary.LittleEndian.PutUint64(seed[i:], rand.Uint64())
	}
	return seed
}

// Seed restarts the generator from seed.
func (s *Source) Seed(seed [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.g = rand.NewChaCha8(seed)
}

// Read fills p; it never fails and never blocks.
func (s *Source) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var w [8]byte
	for i := 0; i < len(p); i += 8 {
		binary.LittleEndian.PutUint64(w[:], s.g.Uint64())
		copy(p[i:], w[:])
	}
	return len(p), nil
}

// Write is accepted, as Linux accepts writes to /dev/urandom, and changes
// nothing here.
func (s *Source) Write(p []byte) (int, error) { return len(p), nil }
