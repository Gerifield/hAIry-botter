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
	if accessToken == "" || verifyToken == "" || appSecret == "" {
		fmt.Println("ACCESS_TOKEN, VERIFY_TOKEN and APP_SECRET must be set")

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

	ms := New(addr, accessToken, verifyToken, appSecret, baseURL, aiSrv)

	log.Printf("Starting server on %s\n", addr)
	if err := ms.ListenAndServe(); err != nil {
		log.Println(err)
	}
}

// FBMessenger .
type FBMessenger struct {
	addr        string
	accessToken string
	verifyToken string
	appSecret   string
	baseURL     string
	aiSrv       string
	httpClient  *http.Client
}

// New .
func New(addr, accessToken, verifyToken, appSecret, baseURL, aiSrv string) *FBMessenger {
	return &FBMessenger{
		addr:        addr,
		accessToken: accessToken,
		verifyToken: verifyToken,
		appSecret:   appSecret,
		baseURL:     baseURL,
		aiSrv:       aiSrv,
		httpClient:  &http.Client{},
	}
}

// ListenAndServe .
func (fbm *FBMessenger) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", fbm.webhook)

	srv := &http.Server{
		Addr:    fbm.addr,
		Handler: mux,
	}

	return srv.ListenAndServe()
}

func (fbm *FBMessenger) webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		log.Printf("invalid method: not get or post")

		return
	}

	// if the method of request is GET
	if r.Method == http.MethodGet {
		// read token from query parameter
		verifyToken := r.URL.Query().Get("hub.verify_token")

		// verify the token included in the incoming request
		if verifyToken != fbm.verifyToken {
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

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to read body: %v", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if !validateSignature(body, signature, fbm.appSecret) {
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

	userMsg := message.Entry[0].Messaging[0].Message.Text
	if userMsg == "" {
		log.Printf("empty message from user: %s", message.Entry[0].Messaging[0].Sender.ID)
		http.Error(w, "empty message from user", http.StatusBadRequest)

		return
	}

	// log.Println("Processing message: ", message.Entry[0].Messaging[0].Message.Text)
	params := url.Values{}
	params.Set("message", userMsg)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/message", fbm.aiSrv), strings.NewReader(params.Encode()))
	if err != nil {
		log.Printf("failed to create request: %v", err)

		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User-ID", fmt.Sprintf("fb-%s", message.Entry[0].Messaging[0].Sender.ID))

	resp, err := fbm.httpClient.Do(req)
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
	err = fbm.sendMessage(message.Entry[0].Messaging[0].Sender.ID, response.Response)
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

func (fbm *FBMessenger) sendMessage(senderId, message string) error {
	// configure the sender ID and message
	var request SendMessage
	request.Recipient.ID = senderId
	request.Message.Text = message

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
	url := fmt.Sprintf("%s/%s?access_token=%s", fbm.baseURL, "me/messages", fbm.accessToken)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed wrap request: %w", err)
	}
	req.Header.Add("Content-Type", "application/json")

	// send http request
	res, err := fbm.httpClient.Do(req)
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
		ID        string `json:"id"`
		Time      int64  `json:"time"`
		Messaging []struct {
			Sender struct {
				ID string `json:"id"`
			} `json:"sender"`
			Recipient struct {
				ID string `json:"id"`
			} `json:"recipient"`
			Timestamp int64 `json:"timestamp"`
			Message   struct {
				Mid         string `json:"mid"`
				Text        string `json:"text"`
				Attachments []struct {
					Type    string `json:"type"`
					Payload struct {
						Title string `json:"title"`
						URL   string `json:"url"`
						// etc.
					} `json:"payload"`
				} `json:"attachments"`
			} `json:"message"`
		} `json:"messaging"`
	} `json:"entry"`
}

// SendMessage data structure for send message
type SendMessage struct {
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
}
