FROM golang:1.23

WORKDIR /app

COPY . .

RUN go build -o bin .

EXPOSE 8081

ENTRYPOINT [ "/app/bin" ]
