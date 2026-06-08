package imageproc

type ThumbnailSize struct {
	Name   string
	Width  int
	Height int
}

var DefaultThumbnailSizes = []ThumbnailSize{
	{Name: "100x100", Width: 100, Height: 100},
	{Name: "300x300", Width: 300, Height: 300},
}
