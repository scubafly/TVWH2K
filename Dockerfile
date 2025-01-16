FROM golang:1.23

ENV TELEGRAM_BOT_TOKEN="bot_token" \
    TELEGRAM_CHAT_ID="chat_id" \
    TOKEN="token"

WORKDIR /app

COPY . .
# remove .env file from image
RUN rm -Rf .env

RUN go build -o bin .

EXPOSE 8081

# TODO change to non root user
# USER 1000

ENTRYPOINT [ "/app/bin" ]
