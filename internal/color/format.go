package color

import (
	"fmt"
	"math"
	"strings"
)

func Format(c RGB, format string) (interface{}, error) {
	switch strings.ToLower(format) {
	case "", "hex":
		return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B), nil
	case "rgb":
		return c, nil
	case "hsl":
		return ToHSL(c), nil
	case "cmyk":
		return ToCMYK(c), nil
	default:
		return nil, fmt.Errorf("unsupported color format: %s", format)
	}
}

func ToHSL(c RGB) HSL {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	maxV := math.Max(r, math.Max(g, b))
	minV := math.Min(r, math.Min(g, b))
	l := (maxV + minV) / 2
	if maxV == minV {
		return HSL{H: 0, S: 0, L: int(math.Round(l * 100))}
	}
	d := maxV - minV
	var h float64
	if l > 0.5 {
		h = d / (2 - maxV - minV)
	} else {
		h = d / (maxV + minV)
	}
	s := h
	switch maxV {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return HSL{
		H: int(math.Round(h * 60)),
		S: int(math.Round(s * 100)),
		L: int(math.Round(l * 100)),
	}
}

func ToCMYK(c RGB) CMYK {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	k := 1 - math.Max(r, math.Max(g, b))
	if k >= 1 {
		return CMYK{K: 100}
	}
	return CMYK{
		C: int(math.Round((1 - r - k) / (1 - k) * 100)),
		M: int(math.Round((1 - g - k) / (1 - k) * 100)),
		Y: int(math.Round((1 - b - k) / (1 - k) * 100)),
		K: int(math.Round(k * 100)),
	}
}
