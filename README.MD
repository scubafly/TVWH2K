# TVWH2K
## A Simple application written in golang that handles webhook from tradingview and sends the data to kraken using the kraken api

## TODO
- [ ] Don't put .env in docker image, inject into the container instead!
- [ ] Output signal to telegram
- [ ] Create authenitcation for webhook
- [ ] Fire order to kraken after webhook call
- [ ] Make connection to kraken
- [ ] Make option to test the connection instead of placing an order
- [ ] Create something that creates an order with stop loss and take profit

- [ ] SH (Should have): Unit tests
- [ ] NTH (NTH): Create trailing stop loss order
- [ ] NTH Create option to change settings like api key, leverage etc. for now in the .env file would be ok

## Done
- [x] SH .env file example
- [x] Put it in a deployable docker container
- [x] Make telegram connection
- [x] Create envoirment variable to store keys, and add to gitignore
- [x] Read envorment variable


## How to run / develop localy
- `go build && go run .` or if you have air installed just `air`
- In your terminal: `curl -X POST http://localhost:8081/webhooks -H 'Content-Type: application/json' -d '{"text": "your webhook data here", "token": "yourtoken"}`
You should see the text in your logs.

## Docker
- `docker build . -t tvwh2k:latest`
- `docker run -p9000:8081 tvwh2k:latest`
