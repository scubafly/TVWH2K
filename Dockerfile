FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/tvwh2k .

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /out/tvwh2k /app/tvwh2k

USER nonroot:nonroot

EXPOSE 8081

ENTRYPOINT ["/app/tvwh2k"]
