FROM golang:1.23.1 AS builder

WORKDIR /app/

COPY go.* ./

RUN go mod download

COPY cmd/main.go cmd/main.go
COPY config/ config/
COPY internal/ internal/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o vault-sa-patcher cmd/main.go

FROM alpine:3.20.3
WORKDIR /app
COPY --from=builder /app/vault-sa-patcher .

EXPOSE 8080
ENTRYPOINT ["/app/vault-sa-patcher"]




