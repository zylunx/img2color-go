package color

import "testing"

func TestMedianCutExtract(t *testing.T) {
	pixels := make([]RGB, 0, 100)
	for i := 0; i < 70; i++ {
		pixels = append(pixels, RGB{R: 250, G: 0, B: 0})
	}
	for i := 0; i < 30; i++ {
		pixels = append(pixels, RGB{R: 0, G: 0, B: 250})
	}
	swatches, err := (MedianCut{}).Extract(pixels, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(swatches) != 2 {
		t.Fatalf("got %d swatches", len(swatches))
	}
	if swatches[0].Ratio < swatches[1].Ratio {
		t.Fatalf("swatches not sorted by ratio: %+v", swatches)
	}
}

func TestKMeansExtract(t *testing.T) {
	pixels := []RGB{{255, 0, 0}, {254, 0, 0}, {0, 0, 255}, {0, 0, 254}}
	swatches, err := (KMeans{Iterations: 4}).Extract(pixels, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(swatches) != 2 {
		t.Fatalf("got %d swatches", len(swatches))
	}
}
