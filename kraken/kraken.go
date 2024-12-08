package kraken

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Kraken struct{}

func NewClient() *Kraken {
	return &Kraken{}
}

type Order struct {
	Nonce     int    `json:"nonce"`
	OrderType string `json:"ordertype"`
	Type      string `json:"type"`
	Volume    string `json:"volume"`
	Pair      string `json:"pair"`
	Price     string `json:"price"`
	ClOrdID   string `json:"cl_ord_id"`
	Test      bool   `json:"test"`
}

func (k *Kraken) AddOrder(order Order) {

	url := "https://api.kraken.com/0/private/AddOrder"
	method := "POST"

	payload := strings.NewReader(fmt.Sprintf(`%s`, order))

	client := &http.Client{}
	req, err := http.NewRequest(method, url, payload)

	if err != nil {
		fmt.Println(err)
		return
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("API-Key", "<API_KEY_VALUE>")
	req.Header.Add("API-Sign", "<API_KEY_VALUE>")

	if order.Test {
		fmt.Printf("URL: %s\n", "https://api.kraken.com/0/private/AddOrder")
		fmt.Printf("METHOD: %s\n", "POST")
		fmt.Printf("PAYLOAD: %v\n", order)
		return
	}

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
}
