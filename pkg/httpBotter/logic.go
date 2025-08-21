package httpBotter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
)

// Logic .
type Logic struct {
	baseURL    string
	httpClient *http.Client
}

// New .
func New(baseURL string) *Logic {
	return &Logic{
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
	}
}

func (l *Logic) Send(userID string, msg string, payload []byte) (string, error) {
	var buff bytes.Buffer

	// Build multipart form data
	mpw := multipart.NewWriter(&buff)
	// Add message
	err := mpw.WriteField("message", msg)
	if err != nil {
		return "", fmt.Errorf("failed to write message filed: %w", err)
	}
	// Add file if exists
	if len(payload) > 0 {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", multipart.FileContentDisposition("payload", "payload.data"))
		h.Set("Content-Type", http.DetectContentType(payload))
		part, err := mpw.CreatePart(h)
		if err != nil {
			return "", fmt.Errorf("failed to create payload file: %w", err)
		}
		_, err = io.Copy(part, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("failed to write payload file: %w", err)
		}
	}

	// Close the writer to finalize the multipart form
	err = mpw.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/message", l.baseURL), &buff)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", mpw.FormDataContentType())
	req.Header.Set("X-User-ID", userID)

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var response struct {
		Response string `json:"response"`
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Debug the response body
		fmt.Println(string(bytes.TrimSpace(b)))
	}

	//err = json.NewDecoder(resp.Body).Decode(&response)
	err = json.NewDecoder(bytes.NewReader(b)).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("failed to decode response body: %w", err)
	}

	return response.Response, nil
}
