package color

import (
	stdcolor "image/color"
)

type RGB struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

type HSL struct {
	H int `json:"h"`
	S int `json:"s"`
	L int `json:"l"`
}

type CMYK struct {
	C int `json:"c"`
	M int `json:"m"`
	Y int `json:"y"`
	K int `json:"k"`
}

type Swatch struct {
	Color RGB     `json:"-"`
	Ratio float64 `json:"ratio"`
}

type Extractor interface {
	Extract(pixels []RGB, n int) ([]Swatch, error)
}

func FromColor(c stdcolor.Color) RGB {
	r, g, b, _ := c.RGBA()
	return RGB{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
}
