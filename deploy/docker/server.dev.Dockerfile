FROM golang:1.26-alpine
RUN go install github.com/air-verse/air@v1.62.0
WORKDIR /src
CMD ["air", "-c", ".air.toml"]
