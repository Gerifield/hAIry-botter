package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.Background(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// Configuration holds the configurable variables
type Configuration struct {
	PollingInterval time.Duration
	WebhookURL      string
	SearchQuery     string
}

func loadConfig() Configuration {
	cfg := Configuration{
		WebhookURL:      "http://localhost:8080/webhook",
		SearchQuery:     "label:Assistantis:unread",
		PollingInterval: 60 * time.Second,
	}

	if val := os.Getenv("WEBHOOK_URL"); val != "" {
		cfg.WebhookURL = val
	}
	if val := os.Getenv("SEARCH_QUERY"); val != "" {
		cfg.SearchQuery = val
	}
	if val := os.Getenv("POLLING_INTERVAL"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			cfg.PollingInterval = time.Duration(i) * time.Second
		} else {
			log.Printf("Invalid POLLING_INTERVAL: %v, using default 60s", err)
		}
	}

	return cfg
}

type emailData struct {
	ID          string
	Sender      string
	Subject     string
	Date        string
	Body        string
	Attachments []attachment
}

type attachment struct {
	Filename string
	Data     []byte
	MimeType string
}

func main() {
	cfg := loadConfig()
	log.Printf("Starting Gmail Reader Script with config: %+v\n", cfg)

	ctx := context.Background()

	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v. Please make sure credentials.json is present in the current directory.", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, gmail.MailGoogleComScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	// Start polling loop
	ticker := time.NewTicker(cfg.PollingInterval)
	defer ticker.Stop()

	// Run once immediately
	pollEmails(srv, cfg)

	for range ticker.C {
		pollEmails(srv, cfg)
	}
}

func pollEmails(srv *gmail.Service, cfg Configuration) {
	log.Printf("Polling emails with query: %s\n", cfg.SearchQuery)
	user := "me"

	r, err := srv.Users.Messages.List(user).Q(cfg.SearchQuery).MaxResults(5).Do()
	if err != nil {
		log.Printf("Unable to retrieve messages: %v", err)
		return
	}

	if len(r.Messages) == 0 {
		log.Println("No matching messages found.")
		return
	}

	for _, m := range r.Messages {
		processMessage(srv, user, m.Id, cfg.WebhookURL)
	}
}

func processMessage(srv *gmail.Service, user string, msgId string, webhookURL string) {
	msg, err := srv.Users.Messages.Get(user, msgId).Format("full").Do()
	if err != nil {
		log.Printf("Unable to get message %s: %v", msgId, err)
		return
	}

	data := extractEmailData(srv, user, msg)

	// Send to webhook
	err = forwardToWebhook(data, webhookURL)
	if err != nil {
		log.Printf("Failed to forward message %s: %v", msgId, err)
		// Leave as unread so it gets picked up again
		return
	}

	// Mark as read
	log.Printf("Message %s forwarded successfully, marking as read.", msgId)
	mods := &gmail.ModifyMessageRequest{
		RemoveLabelIds: []string{"UNREAD"},
	}
	_, err = srv.Users.Messages.Modify(user, msgId, mods).Do()
	if err != nil {
		log.Printf("Unable to modify message %s: %v", msgId, err)
	}
}

func extractEmailData(srv *gmail.Service, user string, msg *gmail.Message) emailData {
	data := emailData{
		ID: msg.Id,
	}

	// Extract headers
	for _, header := range msg.Payload.Headers {
		switch strings.ToLower(header.Name) {
		case "from":
			data.Sender = header.Value
		case "subject":
			data.Subject = header.Value
		case "date":
			data.Date = header.Value
		}
	}

	// Extract body and attachments
	extractParts(srv, user, msg.Id, msg.Payload, &data)

	return data
}

func extractParts(srv *gmail.Service, user string, msgId string, part *gmail.MessagePart, data *emailData) {
	if part.Filename != "" && part.Body != nil && part.Body.AttachmentId != "" {
		// It's an attachment
		attachObj, err := srv.Users.Messages.Attachments.Get(user, msgId, part.Body.AttachmentId).Do()
		if err != nil {
			log.Printf("Unable to get attachment %s for message %s: %v", part.Filename, msgId, err)
			return
		}

		decoded, err := base64.URLEncoding.DecodeString(attachObj.Data)
		if err != nil {
			log.Printf("Unable to decode attachment %s: %v", part.Filename, err)
			return
		}

		data.Attachments = append(data.Attachments, attachment{
			Filename: part.Filename,
			Data:     decoded,
			MimeType: part.MimeType,
		})
	} else if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
		// It's the body
		decoded, err := base64.URLEncoding.DecodeString(part.Body.Data)
		if err == nil {
			data.Body += string(decoded)
		}
	} else if part.MimeType == "text/html" && data.Body == "" && part.Body != nil && part.Body.Data != "" {
		// Fallback to HTML body if plain text is not found yet
		decoded, err := base64.URLEncoding.DecodeString(part.Body.Data)
		if err == nil {
			data.Body += string(decoded)
		}
	}

	// Recursively check parts
	for _, p := range part.Parts {
		extractParts(srv, user, msgId, p, data)
	}
}

func forwardToWebhook(data emailData, webhookURL string) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add metadata fields
	_ = writer.WriteField("Sender", data.Sender)
	_ = writer.WriteField("Subject", data.Subject)
	_ = writer.WriteField("Date", data.Date)

	// Add body if no attachments or to provide context
	if data.Body != "" {
		_ = writer.WriteField("Body", data.Body)
	}

	// Add attachments
	for _, att := range data.Attachments {
		// According to requirements, add as multiple files with name "payload"
		// or whatever array name is expected. I will use "payload" as array name
		// For proper boundary format, we use CreatePart instead of CreateFormFile
		// to set the content-disposition and content-type precisely
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="payload"; filename="%s"`, escapeQuotes(att.Filename))}
		if att.MimeType != "" {
			h["Content-Type"] = []string{att.MimeType}
		} else {
			h["Content-Type"] = []string{"application/octet-stream"}
		}

		part, err := writer.CreatePart(h)
		if err != nil {
			return fmt.Errorf("could not create form file for %s: %v", att.Filename, err)
		}
		_, err = io.Copy(part, bytes.NewReader(att.Data))
		if err != nil {
			return fmt.Errorf("could not copy file data for %s: %v", att.Filename, err)
		}
	}

	err := writer.Close()
	if err != nil {
		return fmt.Errorf("failed to close multipart writer: %v", err)
	}

	req, err := http.NewRequest("POST", webhookURL, body)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned non-2xx status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// escapeQuotes escapes double quotes for header values
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
