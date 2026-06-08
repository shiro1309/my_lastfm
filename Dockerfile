# ---- build stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache upx

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o scrobbler .
RUN upx --best --lzma scrobbler

# ---- runtime stage ----
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/scrobbler .

VOLUME ["/app/data"]
EXPOSE 8080

ENTRYPOINT ["./scrobbler"]