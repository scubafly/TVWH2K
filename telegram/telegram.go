package telegram

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func sendMessage(text string, chatId int32) (string, error) {

	err := godotenv.Load("../.env")
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
	log.Printf("Sending message: %s, to chat id %d", text, chatId)
	var apiUrl string = "https://api.telegram.org/bot" + os.Getenv("TELEGRAM_BOT_TOKEN") + "/sendMessage"

	message := url.Values{
		"text":    {text},
		"chat_id": {strconv.FormatInt(chatId, 10)},
	}

	fmt.Printf("URL: %s\n", apiUrl)
	fmt.Printf("Data: %v\n", message)

	req, err := http.NewRequest(
		http.MethodPost,
		apiUrl,
		strings.NewReader(message.Encode()),
	)
	if err != nil {
		log.Printf("Error sending message: %s", err)
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	// TODO create timout.
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var bodyBytes, errRead = io.ReadAll(resp.Body)
	if errRead != nil {
		log.Printf("Error reading response: %s", errRead)
		return "", errRead
	}
	bodyString := string(bodyBytes)
	log.Printf("Response: %s", bodyString)

	return bodyString, nil
}
