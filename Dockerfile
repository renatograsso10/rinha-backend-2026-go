FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/preprocess ./cmd/preprocess
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/server ./cmd/server
RUN /out/preprocess -in resources/references.json.gz -out /out/index.bin

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/index.bin /app/index.bin
ENV INDEX_PATH=/app/index.bin CLASSIFIER_MODE=forest VISIT_CAP=256 GOMAXPROCS=1 GOMEMLIMIT=64MiB GOGC=100
EXPOSE 8080
ENTRYPOINT ["/app/server"]
