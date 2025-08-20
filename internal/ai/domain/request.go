package domain

type Request struct {
	Message string
	Image   *Image
}
type Image struct {
	MimeType string
	Data     []byte
}
