# Build stage
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/debeasy ./cmd/debeasy

# Runtime stage — distroless static
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/debeasy /app/debeasy
ENV DEBEASY_DATA_DIR=/app/data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/debeasy"]
CMD ["--addr", ":8080"]
