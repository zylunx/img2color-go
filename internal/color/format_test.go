package color

import "testing"

func TestFormat(t *testing.T) {
	c := RGB{R: 255, G: 107, B: 107}
	got, err := Format(c, "hex")
	if err != nil {
		t.Fatal(err)
	}
	if got != "#FF6B6B" {
		t.Fatalf("hex = %v", got)
	}
	if hsl := ToHSL(c); hsl.H != 0 || hsl.S == 0 || hsl.L == 0 {
		t.Fatalf("unexpected hsl: %+v", hsl)
	}
	if cmyk := ToCMYK(RGB{}); cmyk.K != 100 {
		t.Fatalf("black cmyk = %+v", cmyk)
	}
}
