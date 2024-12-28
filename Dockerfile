FROM golang:1.23

WORKDIR /app

COPY . .

RUN go build -o bin .

ENTRYPOINT [ "/app/bin" ]
