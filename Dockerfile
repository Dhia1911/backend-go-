FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o app .
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/app .
ENV PORT=8080
EXPOSE $PORT
CMD ["./app"]