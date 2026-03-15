package utils

import (
	"math/rand"
	"sort"
)

// RandomOffset returns a random pixel offset within the given radius.
// Used for interaction attempts (entrances, objects) where small jitter
// around the target position is sufficient.
func RandomOffset(radius int) (int, int) {
	x := RandRng(-radius, radius)
	y := RandRng(-radius, radius)
	return x, y
}

// PickupProbeOffsets generates a list of probe offsets for item pickup.
// Offsets are organized by distance from center (closest first), shuffled
// within each distance ring, with ±1px jitter for anti-heuristic randomness.
// This guarantees systematic coverage while appearing random.
func PickupProbeOffsets(radius int) [][2]int {
	const step = 2

	// Group grid offsets by squared distance from center
	rings := make(map[int][][2]int)
	for x := -radius; x <= radius; x += step {
		for y := -radius; y <= radius; y += step {
			d := x*x + y*y
			rings[d] = append(rings[d], [2]int{x + RandRng(-1, 1), y + RandRng(-1, 1)})
		}
	}

	// Sort distances so we probe center outward
	dists := make([]int, 0, len(rings))
	for d := range rings {
		dists = append(dists, d)
	}
	sort.Ints(dists)

	// Build final list: expanding rings, each internally shuffled
	offsets := make([][2]int, 0, 25)
	for _, d := range dists {
		ring := rings[d]
		rand.Shuffle(len(ring), func(i, j int) {
			ring[i], ring[j] = ring[j], ring[i]
		})
		offsets = append(offsets, ring...)
	}

	return offsets
}
