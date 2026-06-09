package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// TestDecodeReturnsImageDimensions проверяет чтение размеров изображения
func TestDecodeReturnsImageDimensions(t *testing.T) {
	data := testPNG(t, 4, 6)

	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	if decoded.Width != 4 || decoded.Height != 6 {
		t.Fatalf("dimensions = %dx%d, want 4x6", decoded.Width, decoded.Height)
	}
}

// TestBuildThumbnailsCreatesExpectedSizes проверяет создание thumbnails
func TestBuildThumbnailsCreatesExpectedSizes(t *testing.T) {
	decoded, err := Decode(bytes.NewReader(testPNG(t, 8, 8)))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	thumbnails, err := BuildThumbnails(decoded.Image, []ThumbnailSize{
		{Name: "2x2", Width: 2, Height: 2},
		{Name: "3x3", Width: 3, Height: 3},
	})
	if err != nil {
		t.Fatalf("BuildThumbnails returned error: %v", err)
	}
	if len(thumbnails) != 2 {
		t.Fatalf("len(thumbnails) = %d, want 2", len(thumbnails))
	}
	for _, thumbnail := range thumbnails {
		if thumbnail.ContentType != "image/png" {
			t.Fatalf("ContentType = %q, want image/png", thumbnail.ContentType)
		}
		decodedThumbnail, err := Decode(bytes.NewReader(thumbnail.Data))
		if err != nil {
			t.Fatalf("decode thumbnail: %v", err)
		}
		if decodedThumbnail.Width != thumbnail.Size.Width || decodedThumbnail.Height != thumbnail.Size.Height {
			t.Fatalf("thumbnail dimensions = %dx%d, want %dx%d", decodedThumbnail.Width, decodedThumbnail.Height, thumbnail.Size.Width, thumbnail.Size.Height)
		}
	}
}

// TestDecodeRejectsInvalidImage проверяет ошибку декодирования
func TestDecodeRejectsInvalidImage(t *testing.T) {
	_, err := Decode(bytes.NewReader([]byte("not-image")))
	if err == nil {
		t.Fatal("Decode should return error")
	}
}

// testPNG создает PNG fixture заданного размера
func testPNG(t *testing.T, width int, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return body.Bytes()
}
