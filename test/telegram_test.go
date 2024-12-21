import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSendMessage(t *testing.T) {
	var msg = Message {
		Text: "Hello World",
		Chat: chat
	}

	requestBody, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("Error encoding request body, got %s", err.Error())
	}

	sm, err := sendMessage(requestBody, 9000)
	if err != nil {
		t.Errorf("Error sending message to Telegram, got %s", err.Error())
	}

}
