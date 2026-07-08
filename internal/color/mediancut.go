package color

import (
	"errors"
	"sort"
)

type MedianCut struct{}

type box struct {
	pixels []RGB
}

func (MedianCut) Extract(pixels []RGB, n int) ([]Swatch, error) {
	if len(pixels) == 0 {
		return nil, errors.New("no pixels to extract")
	}
	if n <= 0 {
		n = 1
	}
	boxes := []box{{pixels: append([]RGB(nil), pixels...)}}
	for len(boxes) < n {
		idx := largestBox(boxes)
		if idx < 0 || len(boxes[idx].pixels) < 2 {
			break
		}
		a, b := splitBox(boxes[idx])
		boxes = append(append(boxes[:idx], boxes[idx+1:]...), a, b)
	}
	total := float64(len(pixels))
	out := make([]Swatch, 0, len(boxes))
	for _, b := range boxes {
		out = append(out, Swatch{Color: average(b.pixels), Ratio: float64(len(b.pixels)) / total})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ratio > out[j].Ratio })
	return out, nil
}

func largestBox(boxes []box) int {
	best, score := -1, -1
	for i, b := range boxes {
		rng := channelRange(b.pixels)
		s := max3(rng[0], rng[1], rng[2]) * len(b.pixels)
		if s > score {
			best, score = i, s
		}
	}
	return best
}

func splitBox(b box) (box, box) {
	p := b.pixels
	rng := channelRange(p)
	channel := 0
	if rng[1] >= rng[0] && rng[1] >= rng[2] {
		channel = 1
	} else if rng[2] >= rng[0] && rng[2] >= rng[1] {
		channel = 2
	}
	sort.Slice(p, func(i, j int) bool {
		switch channel {
		case 0:
			return p[i].R < p[j].R
		case 1:
			return p[i].G < p[j].G
		default:
			return p[i].B < p[j].B
		}
	})
	mid := len(p) / 2
	return box{pixels: append([]RGB(nil), p[:mid]...)}, box{pixels: append([]RGB(nil), p[mid:]...)}
}

func channelRange(pixels []RGB) [3]int {
	minR, minG, minB := 255, 255, 255
	maxR, maxG, maxB := 0, 0, 0
	for _, p := range pixels {
		minR, maxR = min(minR, int(p.R)), max(maxR, int(p.R))
		minG, maxG = min(minG, int(p.G)), max(maxG, int(p.G))
		minB, maxB = min(minB, int(p.B)), max(maxB, int(p.B))
	}
	return [3]int{maxR - minR, maxG - minG, maxB - minB}
}

func average(pixels []RGB) RGB {
	var r, g, b int
	for _, p := range pixels {
		r += int(p.R)
		g += int(p.G)
		b += int(p.B)
	}
	l := len(pixels)
	return RGB{R: uint8(r / l), G: uint8(g / l), B: uint8(b / l)}
}

func max3(a, b, c int) int { return max(a, max(b, c)) }
