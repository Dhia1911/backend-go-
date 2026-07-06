FROM golang:1.21-alpine AS builder
WORKDIR /app
ENV PORT=8080
EXPOSE $PORT
COPY go.mod go.sum .
RUN go mod download
COPY . .
RUN go build -o app .

FROM alpine:3.19
WORKDIR /app
ENV PORT=8080
EXPOSE $PORT
COPY --from=builder /app/app .
CMD ["./app"]