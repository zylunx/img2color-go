package color

import (
	"errors"
	"math"
	"sort"
)

type KMeans struct {
	Iterations int
}

func (k KMeans) Extract(pixels []RGB, n int) ([]Swatch, error) {
	if len(pixels) == 0 {
		return nil, errors.New("no pixels to extract")
	}
	if n <= 0 {
		n = 1
	}
	if n > len(pixels) {
		n = len(pixels)
	}
	iterations := k.Iterations
	if iterations <= 0 {
		iterations = 12
	}
	centers := seedCenters(pixels, n)
	assignments := make([]int, len(pixels))
	for it := 0; it < iterations; it++ {
		sums := make([][4]int, n)
		for i, p := range pixels {
			idx := nearest(p, centers)
			assignments[i] = idx
			sums[idx][0] += int(p.R)
			sums[idx][1] += int(p.G)
			sums[idx][2] += int(p.B)
			sums[idx][3]++
		}
		for i, s := range sums {
			if s[3] == 0 {
				continue
			}
			centers[i] = RGB{R: uint8(s[0] / s[3]), G: uint8(s[1] / s[3]), B: uint8(s[2] / s[3])}
		}
	}
	counts := make([]int, n)
	for _, a := range assignments {
		counts[a]++
	}
	out := make([]Swatch, 0, n)
	total := float64(len(pixels))
	for i, c := range centers {
		if counts[i] > 0 {
			out = append(out, Swatch{Color: c, Ratio: float64(counts[i]) / total})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ratio > out[j].Ratio })
	return out, nil
}

func seedCenters(pixels []RGB, n int) []RGB {
	step := len(pixels) / n
	centers := make([]RGB, n)
	for i := range centers {
		centers[i] = pixels[min(i*step, len(pixels)-1)]
	}
	return centers
}

func nearest(p RGB, centers []RGB) int {
	best := 0
	bestDist := math.MaxFloat64
	for i, c := range centers {
		d := distance(p, c)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

func distance(a, b RGB) float64 {
	dr := float64(int(a.R) - int(b.R))
	dg := float64(int(a.G) - int(b.G))
	db := float64(int(a.B) - int(b.B))
	return dr*dr + dg*dg + db*db
}
