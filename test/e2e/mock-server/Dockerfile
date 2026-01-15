FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY main.go .
RUN CGO_ENABLED=0 go build -o mock-server main.go

FROM alpine:3.21

COPY --from=builder /app/mock-server /mock-server
EXPOSE 8080

CMD ["/mock-server"]
