// Command mkicon draws a plain grid icon: go run tools/mkicon.go out.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	const n = 128
	img := image.NewGray(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for k := 0; k <= 3; k++ {
		p := 8 + k*(n-16)/3
		w := 2
		if k == 0 || k == 3 {
			w = 5
		}
		for t := 0; t < w; t++ {
			for i := 8; i < n-8+w; i++ {
				img.SetGray(min(p+t, n-1), min(i, n-1), color.Gray{0})
				img.SetGray(min(i, n-1), min(p+t, n-1), color.Gray{0})
			}
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	png.Encode(f, img)
}
