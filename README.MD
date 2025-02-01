# TVWH2K
## A Simple application written in golang that handles webhook from tradingview and sends the data to kraken using the kraken api

## TODO
- [ ] Output signal to telegram
- [ ] Fire order to kraken after webhook call
- [ ] Make connection to kraken
- [ ] Make option to test the connection instead of placing an order
- [ ] Create something that creates an order with stop loss and take profit

- [ ] SH (Should have): Unit tests
- [ ] NTH (NTH): Create trailing stop loss order
- [ ] NTH Create option to change settings like api key, leverage etc. for now in the .env file would be ok

## Done
- [x] Create ~~authentication~~ token validation for webhook
- [x] Don't put .env in docker image, inject into the container instead!
- [x] SH .env file example
- [x] Put it in a deployable docker container
- [x] Make telegram connection
- [x] Create environment variable to store keys, and add to gitignore
- [x] Read environment variable


## How to run / develop locally
- `go build && go run .` or if you have air installed just `air`
- In your terminal: `curl -X POST http://localhost:8081/webhooks -H 'Content-Type: application/json' -d '{"text": "your webhook data here", "token": "yourtoken"}`
You should see the text in your logs.

## Docker
- `docker build . -t scubafly/tvwh2k:latest`
- `docker run -p9000:8081 -e TELEGRAM_BOT_TOKEN=token -e TELEGRAM_CHAT_ID=12345 -e TOKEN=abc scubafly/tvwh2k:latest`
- `docker compose up --build` when the .env file is used.

### To build for arm64 to run on linux you can use buildx
`docker buildx build --platform linux/amd64 -t scubafly/tvwh2k:latest`
