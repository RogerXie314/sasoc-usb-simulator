// +build ignore

package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func fillCircle(img *image.RGBA, cx, cy, r int, c color.Color) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				img.Set(cx+x, cy+y, c)
			}
		}
	}
}

func thickLine(img *image.RGBA, x1, y1, x2, y2, w int, c color.Color) {
	dx := x2 - x1
	dy := y2 - y1
	steps := 500
	for s := 0; s <= steps; s++ {
		t := float64(s) / float64(steps)
		x := int(float64(x1) + float64(dx)*t)
		y := int(float64(y1) + float64(dy)*t)
		fillCircle(img, x, y, w, c)
	}
}

func drawCheckmark(img *image.RGBA, cx, cy, size int, c color.Color) {
	thickLine(img, cx-size/2, cy+size/4, cx, cy+size/2, size/6, c)
	thickLine(img, cx, cy+size/2, cx+size/2, cy-size/4, size/6, c)
}

func main() {
	// ICO spec: width/height stored as uint8, max 256.
	// Use 256x256 to be safe and compatible.
	size := 256
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	cx, cy := size/2, size/2

	// Colors
	blue := color.RGBA{0x1a, 0x7a, 0xd4, 0xff}
	blueLight := color.RGBA{0x4a, 0xa8, 0xff, 0xff}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	green := color.RGBA{0x52, 0xc4, 0x1a, 0xff}

	// Fill background blue
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, blue)
		}
	}

	// White outer ring
	fillCircle(img, cx, cy, 120, white)

	// Inner blue gradient
	fillCircle(img, cx, cy, 110, blueLight)

	// Draw bold "S" letter (white)
	thickLine(img, cx-45, cy-40, cx+45, cy-40, 12, white)   // top
	thickLine(img, cx+45, cy+40, cx-45, cy+40, 12, white)   // bottom
	thickLine(img, cx-45, cy, cx+45, cy, 12, white)         // middle
	thickLine(img, cx-45, cy-40, cx-45, cy, 12, white)      // left-top to middle
	thickLine(img, cx+45, cy, cx+45, cy+40, 12, white)      // middle to right-bottom

	// Green checkmark at bottom right
	drawCheckmark(img, cx+70, cy+70, 40, green)

	// Save PNG
	rootDir := filepath.Join(os.Getenv("GOPATH"), "src", "github.com", "usb-simulator")
	if rootDir == "" || rootDir == filepath.Join("src", "github.com", "usb-simulator") {
		// fallback: try relative to current dir
		rootDir = filepath.Dir(filepath.Dir(os.Args[0]))
		if rootDir == "." {
			rootDir = filepath.Dir(os.Args[0])
		}
	}
	// Use hardcoded project root as reliable fallback
	rootDir = `D:\IEG&EDR&ARS\USM\SASOC-V200R026C01\移动安检站\usb-simulator`

	pngPath := filepath.Join(rootDir, "icon_raw.png")
	f, err := os.Create(pngPath)
	if err != nil {
		fmt.Println("Error creating PNG:", err)
		return
	}
	png.Encode(f, img)
	f.Close()
	fmt.Println("Saved PNG:", pngPath)

	// Convert PNG to ICO (standard ICO format with embedded PNG data)
	icoPath := filepath.Join(rootDir, "icon.ico")
	if err := pngToICO(pngPath, icoPath); err != nil {
		fmt.Println("ICO conversion failed:", err)
		return
	}
	fmt.Println("Saved ICO:", icoPath)
}

// pngToICO converts a PNG file to a standard Windows ICO file.
// The PNG data is embedded directly inside the ICO (PNG-in-ICO format,
// supported since Windows Vista).
func pngToICO(pngPath, icoPath string) error {
	pngData, err := os.ReadFile(pngPath)
	if err != nil {
		return err
	}

	f, err := os.Create(icoPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// ---- ICO Header (6 bytes) ----
	binary.Write(f, binary.LittleEndian, uint16(0))  // Reserved
	binary.Write(f, binary.LittleEndian, uint16(1))  // Type: 1 = icon
	binary.Write(f, binary.LittleEndian, uint16(1))  // Count: 1 image

	// ---- ICONDIRENTRY (16 bytes) ----
	// Width/Height as uint8: 0 means 256 pixels
	binary.Write(f, binary.LittleEndian, uint8(0))    // Width  (0 = 256)
	binary.Write(f, binary.LittleEndian, uint8(0))    // Height (0 = 256)
	binary.Write(f, binary.LittleEndian, uint8(0))    // ColorCount (0 = >256 colors)
	binary.Write(f, binary.LittleEndian, uint8(0))    // Reserved
	binary.Write(f, binary.LittleEndian, uint16(1))   // Planes
	binary.Write(f, binary.LittleEndian, uint16(32))  // Bits per pixel
	binary.Write(f, binary.LittleEndian, uint32(len(pngData))) // Size of image data
	binary.Write(f, binary.LittleEndian, uint32(22))  // Offset to image data (6 + 16)

	// ---- Image data (raw PNG bytes) ----
	_, err = f.Write(pngData)
	return err
}
