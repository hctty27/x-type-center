FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/x-type-center ./cmd/server

FROM alpine:3.22
RUN adduser -D -H -u 10001 app
USER app
COPY --from=build /out/x-type-center /usr/local/bin/x-type-center
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/x-type-center"]
