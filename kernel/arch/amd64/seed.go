//go:build burrow

package amd64

// seed.go - 32 bytes to seed the kernel's random source (W14-S03,
// kernel/random): RDRAND when CPUID says the CPU has it, else TSC readings
// run through splitmix64. Never for cryptography (see package random).

// Implemented in seed_amd64.s.
func cpuid(leaf, sub uint32) (a, b, c, d uint32)
func rdrand() (v uint64, ok bool)
func rdtsc() uint64

const (
	cpuidRDRAND  = 1 << 30 // CPUID.1:ECX
	rdrandTries  = 10      // Intel's advice before giving up on a word
	splitmixStep = 0x9E3779B97F4A7C15
)

// Seed returns the seed and whether RDRAND supplied it.
func Seed() (seed [32]byte, fromRDRAND bool) {
	_, _, ecx, _ := cpuid(1, 0)
	fromRDRAND = ecx&cpuidRDRAND != 0
	for i := 0; i < len(seed); i += 8 {
		w, ok := uint64(0), false
		for try := 0; fromRDRAND && !ok && try < rdrandTries; try++ {
			w, ok = rdrand()
		}
		if !ok {
			fromRDRAND = false
			w = splitmix(rdtsc() + uint64(i)*splitmixStep)
		}
		for b := 0; b < 8; b++ {
			seed[i+b] = byte(w >> (8 * b))
		}
	}
	return seed, fromRDRAND
}

func splitmix(x uint64) uint64 {
	x += splitmixStep
	x = (x ^ x>>30) * 0xBF58476D1CE4E5B9
	x = (x ^ x>>27) * 0x94D049BB133111EB
	return x ^ x>>31
}
