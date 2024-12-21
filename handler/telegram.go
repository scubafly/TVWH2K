package handler

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

type Message struct {
	Text string `json:"text"`
	Chat Chat   `json:"chat"`
}

type Chat struct {
	Id int `json:"id"`
}

func sendMessage(message string, chatId int64) (string, error) {

	log.Printf("Sending message: %s, to chat id %d", message, chatId)
	var api string = "https://api.telegram.org/bot" + os.Getenv(TELEGRAM_BOT_TOKEN) + "/sendMessage"
	resp, err := http.PostForm(
		api,
		url.Values{
			"chat_id": {strconv.FormatInt(chatId, 10)},
			"text":    {message},
		})

	if err != nil {
		log.Printf("Error sending message: %s", err)
		return "", err
	}
	defer resp.Body.Close()

	var bodyBytes, errRead = ioutil.ReadAll(resp.Body)
	if errRead != nil {
		log.Printf("Error reading response: %s", errRead)
		return "", errRead
	}
	bodyString := string(bodyBytes)
	log.Printf("Response: %s", bodyString)

	return bodyString, nil
}
