FROM golang:1.27.1-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY main.go main_test.go ./
COPY templates ./templates
COPY static ./static
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/groupie-tracker .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app \
    && mkdir -p /app/data \
    && chown app:app /app/data
WORKDIR /app
COPY --from=build /out/groupie-tracker ./groupie-tracker
USER app
EXPOSE 8080
ENTRYPOINT ["./groupie-tracker"]
