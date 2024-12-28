# TVWH2K
## A Simple application written in golang that handles webhook from tradingview and sends the data to kraken using the kraken api

## TODO
- [ ] Output signal to telegram
- [ ] Create authenitcation for webhook
- [ ] Fire order to kraken after webhook call
- [ ] Make connection to kraken
- [ ] Make option to test the connection instead of placing an order
- [ ] Put it in a deployable docker container
- [ ] Create something that creates an order with stop loss and take profit
- [ ] SH .env file example
- [ ] SH (Should have): Unit tests
- [ ] NTH (NTH): Create trailing stop loss order
- [ ] NTH Create option to change settings like api key, leverage etc. for now in the .env file would be ok

## Done
- [x] Make telegram connection
- [x] Create envoirment variable to store keys, and add to gitignore
- [x] Read envorment variable


## How to run / develop localy
- `go build && go run .`
- In your terminal: `curl -X POST http://localhost:8081/webhooks -H 'Content-Type: application/json' -d '{"text": "your webhook data here"}`
You should see the text in your logs.
