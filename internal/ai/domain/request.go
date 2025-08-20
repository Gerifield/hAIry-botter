package domain

type Request struct {
	Message    string
	InlineData *InlineData
}
type InlineData struct {
	MimeType string
	Data     []byte
}
