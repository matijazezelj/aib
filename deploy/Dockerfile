FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /aib ./cmd/aib

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /aib /usr/local/bin/aib

RUN mkdir -p /data
VOLUME /data

EXPOSE 8080

ENTRYPOINT ["aib"]
CMD ["serve", "--listen", ":8080"]
