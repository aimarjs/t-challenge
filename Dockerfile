FROM golang:1.16 as builder

WORKDIR /app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -v -o app

FROM alpine:latest  
WORKDIR /root/
COPY --from=builder /app/app .

CMD ["./app"]