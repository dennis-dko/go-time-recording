FROM golang:1.24

RUN mkdir /src/
WORKDIR /src/
COPY . .
RUN go get ./...
RUN go build -ldflags "-linkmode external -extldflags -static" -a cmd/main.go

FROM alpine:latest
RUN apk add --no-cache tzdata ca-certificates
COPY --from=0 /src/cmd/main /main
COPY --from=0 /src/cmd/configs /configs
EXPOSE 9090

CMD ["/main"]