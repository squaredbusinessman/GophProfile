package imageproc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"

	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

type ThumbnailSize struct {
	Name   string
	Width  int
	Height int
}

var DefaultThumbnailSizes = []ThumbnailSize{
	{Name: "100x100", Width: 100, Height: 100},
	{Name: "300x300", Width: 300, Height: 300},
}

type DecodedImage struct {
	Image  image.Image
	Width  int
	Height int
}

type Thumbnail struct {
	Size        ThumbnailSize
	ContentType string
	Data        []byte
}

// Decode читает изображение и возвращает размеры оригинала
func Decode(reader io.Reader) (DecodedImage, error) {
	img, _, err := image.Decode(reader)
	if err != nil {
		return DecodedImage{}, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	return DecodedImage{
		Image:  img,
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
	}, nil
}

// BuildThumbnails создает PNG thumbnails заданных размеров
func BuildThumbnails(img image.Image, sizes []ThumbnailSize) ([]Thumbnail, error) {
	thumbnails := make([]Thumbnail, 0, len(sizes))
	for _, size := range sizes {
		resized := resizeCover(img, size.Width, size.Height)

		var body bytes.Buffer
		if err := png.Encode(&body, resized); err != nil {
			return nil, fmt.Errorf("encode thumbnail %s: %w", size.Name, err)
		}

		thumbnails = append(thumbnails, Thumbnail{
			Size:        size,
			ContentType: "image/png",
			Data:        body.Bytes(),
		})
	}

	return thumbnails, nil
}

// resizeCover масштабирует изображение с заполнением целевого размера
func resizeCover(src image.Image, width int, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()
	cropWidth := srcWidth
	cropHeight := srcHeight
	offsetX := 0
	offsetY := 0

	if srcWidth*height > srcHeight*width {
		cropWidth = srcHeight * width / height
		offsetX = (srcWidth - cropWidth) / 2
	} else if srcWidth*height < srcHeight*width {
		cropHeight = srcWidth * height / width
		offsetY = (srcHeight - cropHeight) / 2
	}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sourceX := srcBounds.Min.X + offsetX + x*cropWidth/width
			sourceY := srcBounds.Min.Y + offsetY + y*cropHeight/height
			dst.Set(x, y, normalizeColor(src.At(sourceX, sourceY)))
		}
	}

	return dst
}

// normalizeColor приводит цвет к RGBA для стабильного PNG output
func normalizeColor(value color.Color) color.Color {
	r, g, b, a := value.RGBA()
	return color.RGBA{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: uint8(a >> 8),
	}
}
