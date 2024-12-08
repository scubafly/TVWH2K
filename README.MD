# TVWH2K
## A Simple application written in golang that handles webhook from tradingview and sends the data to kraken using the kraken api

## TODO
- [ ] Create authenitcation
- [ ] Make connection to kraken
- [ ] Create envoirment variable to store keys
- [ ] Put it in a deployable docker container

## How to run / develop localy
- `go build && go run .`
- In your terminal: `curl -X POST http://localhost:8081/webhooks -H 'Content-Type: application/json' -d '{"text": "your webhook data here"}`
You should see the text in your logs.
