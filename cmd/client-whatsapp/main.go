package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func main() {

	accessToken := os.Getenv("ACCESS_TOKEN")
	verifyToken := os.Getenv("VERIFY_TOKEN")
	appSecret := os.Getenv("APP_SECRET")
	whatsappBusinessPhoneID := os.Getenv("WHATSAPP_BUSINESS_PHONE_ID")
	if accessToken == "" || verifyToken == "" || appSecret == "" || whatsappBusinessPhoneID == "" {
		fmt.Println("ACCESS_TOKEN, VERIFY_TOKEN, APP_SECRET and WHATSAPP_BUSINESS_PHONE_ID must be set")

		return
	}

	baseURL := os.Getenv("GRAPHQL_URL")
	if baseURL == "" {
		baseURL = "https://graph.facebook.com/v22.0"
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8082"
	}

	aiSrv := os.Getenv("AI_SERVICE")
	if aiSrv == "" {
		aiSrv = "http://127.0.0.1:8080"
	}

	ms := New(addr, accessToken, verifyToken, appSecret, baseURL, whatsappBusinessPhoneID, aiSrv)

	log.Printf("Starting server on %s\n", addr)
	if err := ms.ListenAndServe(); err != nil {
		log.Println(err)
	}
}

// WhatsappMessenger .
type WhatsappMessenger struct {
	addr        string
	accessToken string
	verifyToken string
	appSecret   string
	baseURL     string
	aiSrv       string
	phoneID     string
	httpClient  *http.Client
}

// New .
func New(addr, accessToken, verifyToken, appSecret, baseURL, phoneID, aiSrv string) *WhatsappMessenger {
	return &WhatsappMessenger{
		addr:        addr,
		accessToken: accessToken,
		verifyToken: verifyToken,
		appSecret:   appSecret,
		baseURL:     baseURL,
		aiSrv:       aiSrv,
		phoneID:     phoneID,
		httpClient:  &http.Client{},
	}
}

// ListenAndServe .
func (wm *WhatsappMessenger) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", wm.webhook)

	srv := &http.Server{
		Addr:    wm.addr,
		Handler: mux,
	}

	return srv.ListenAndServe()
}

func (wm *WhatsappMessenger) webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		log.Printf("invalid method: not get or post")

		return
	}

	// if the method of request is GET
	if r.Method == http.MethodGet {
		// read token from query parameter
		verifyToken := r.URL.Query().Get("hub.verify_token")

		// verify the token included in the incoming request
		if verifyToken != wm.verifyToken {
			log.Printf("invalid verification token: %s", verifyToken)

			return
		}

		// write string from challenge query parameter
		if _, err := w.Write([]byte(r.URL.Query().Get("hub.challenge"))); err != nil {
			log.Printf("failed to write response body: %v", err)
		}

		return
	}

	// Validate the payload
	// https://developers.facebook.com/docs/messenger-platform/webhooks#validate-payloads
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		log.Println("missing X-Hub-Signature-256 header")
		http.Error(w, "missing X-Hub-Signature-256 header", http.StatusBadRequest)

		// Debug output
		fmt.Println(r.Header)
		io.Copy(os.Stdout, r.Body) // Log the body for debugging purposes

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to read body: %v", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if !validateSignature(body, signature, wm.appSecret) {
		log.Println("invalid X-Hub-Signature header")
		http.Error(w, "invalid X-Hub-Signature header", http.StatusBadRequest)
		return
	}

	// initiate Message data structure to message variable
	var message Message
	err = json.Unmarshal(body, &message)
	if err != nil {
		log.Printf("failed to unmarshal body: %v", err)
		http.Error(w, "failed to unmarshal body", http.StatusBadRequest)
		return
	}

	userMsg := message.Entry[0].Changes[0].Value.Messages[0].Text.Body
	if userMsg == "" {
		log.Printf("empty message from user: %s", message.Entry[0].Changes[0].Value.Messages[0].ID)
		http.Error(w, "empty message from user", http.StatusBadRequest)

		return
	}

	// log.Println("Processing message: ", message.Entry[0].Messaging[0].Message.Text)
	params := url.Values{}
	params.Set("message", userMsg)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/message", wm.aiSrv), strings.NewReader(params.Encode()))
	if err != nil {
		log.Printf("failed to create request: %v", err)

		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User-ID", fmt.Sprintf("whatsapp-%s", message.Entry[0].Changes[0].Value.Messages[0].ID))

	resp, err := wm.httpClient.Do(req)
	if err != nil {
		log.Printf("failed to send request: %v", err)

		return
	}
	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("failed to read response body: %v", err)

		return
	}

	var response struct {
		Response string `json:"response"`
	}
	err = json.Unmarshal(b, &response)
	if err != nil {
		log.Printf("failed to decode response: %v, body: %s", err, string(b))

		return
	}

	// send message to end-user
	err = wm.sendMessage(message.Entry[0].Changes[0].Value.Messages[0].From, response.Response)
	if err != nil {
		log.Printf("failed to send message: %v", err)
	}

	return
}

func validateSignature(content []byte, signature string, appSecret string) bool {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(content)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))
	if len(signature) < 7 {
		return false
	}

	return hmac.Equal([]byte(signature[7:]), []byte(expectedMAC))
}

func (wm *WhatsappMessenger) sendMessage(senderNumber, message string) error {
	// configure the sender ID and message
	var request SendMessage
	request.To = senderNumber
	request.MessagingProduct = "whatsapp"
	request.RecipientType = "individual"
	request.Type = "text"
	request.Text.Body = message

	// validate empty message
	if len(message) == 0 {
		return errors.New("message can't be empty")
	}

	// marshal request data
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("error marshall request: %w", err)
	}

	// setup http request
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/%s/messages", wm.baseURL, wm.phoneID, wm.accessToken), bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed wrap request: %w", err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", wm.accessToken))

	// send http request
	res, err := wm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed send request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	// print response
	// log.Printf("message sent successfully?\n%#v", res)

	return nil
}

// Message data structure for message event
type Message struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Value struct {
				MessagingProduct string `json:"messaging_product"`
				Metadata         struct {
					DisplayPhoneNumber string `json:"display_phone_number"`
					PhoneNumberID      string `json:"phone_number_id"`
				} `json:"metadata"`
				Contacts []struct {
					Profile struct {
						Name string `json:"name"`
					} `json:"profile"`
					WaID string `json:"wa_id"`
				} `json:"contacts"`
				Messages []struct {
					From      string `json:"from"`
					ID        string `json:"id"`
					Timestamp string `json:"timestamp"`
					Text      struct {
						Body string `json:"body"`
					} `json:"text"`
					Type string `json:"type"`
				} `json:"messages"`
			} `json:"value"`
			Field string `json:"field"`
		} `json:"changes"`
	} `json:"entry"`
}

// SendMessage data structure for send message
type SendMessage struct {
	MessagingProduct string `json:"messaging_product"`
	RecipientType    string `json:"recipient_type"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Text             struct {
		PreviewURL bool   `json:"preview_url"`
		Body       string `json:"body"`
	} `json:"text"`
}
