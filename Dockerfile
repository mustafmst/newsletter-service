FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/newsletter ./cmd/newsletter

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/newsletter /app/newsletter
EXPOSE 8080
ENTRYPOINT ["/app/newsletter"]
