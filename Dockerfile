FROM golang:1.21-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/img2color ./cmd/server

FROM alpine:3.20
RUN adduser -D -H appuser
USER appuser
WORKDIR /app
COPY --from=build /out/img2color /app/img2color
EXPOSE 8080
CMD ["/app/img2color"]
