FROM golang:1.11 AS builder

WORKDIR $GOPATH/src/github.com/gernest/apidemic
COPY . .

ENV CGO_ENABLED 0
ENV GOOS linux

RUN go get ./cmd/apidemic
RUN go build -o /apidemic cmd/apidemic/main.go

FROM alpine:latest

WORKDIR /root/
COPY --from=builder /apidemic .
CMD ["./apidemic", "start"]
