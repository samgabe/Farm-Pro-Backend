FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /out/server /app/server
COPY db/schema.sql /app/db/schema.sql

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/app/server"]
