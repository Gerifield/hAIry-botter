package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
)

func main() {
	reader := bufio.NewReader(os.Stdin)
	spin := spinner.New(spinner.CharSets[14], 100*time.Millisecond)

	botSrv := http.DefaultClient
	serverURL := "http://127.0.0.1:8080"
	if os.Getenv("SERVER_URL") != "" {
		serverURL = os.Getenv("SERVER_URL")
	}

	for {
		fmt.Print("> ")
		input, _, err := reader.ReadLine()
		if err != nil {
			fmt.Println("Error reading input:", err)
			return
		}
		spin.Start()

		// Send to AI server
		response, err := callServer(botSrv, serverURL, string(input))
		spin.Stop()
		if err != nil {
			fmt.Println("Error during receiving response:", err)

			continue
		}

		fmt.Print(">> ", response, "\n")
	}
}

func callServer(client *http.Client, baseURL string, input string) (string, error) {
	form := url.Values{}
	form.Set("message", input)

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/message", baseURL), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-User-ID", "client-cli")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var response struct {
		Response string `json:"response"`
	}

	err = json.NewDecoder(resp.Body).Decode(&response)
	return response.Response, err
}
