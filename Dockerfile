FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tvwh2k .

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /out/tvwh2k /app/tvwh2k

USER nonroot:nonroot

EXPOSE 8081

ENTRYPOINT ["/app/tvwh2k"]
