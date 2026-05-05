FROM --platform=$BUILDPLATFORM alpine:3.20 AS build
RUN apk add --no-cache build-base
WORKDIR /src
COPY cserver ./cserver
RUN mkdir -p /out && gcc -O3 -flto -static -pthread -o /out/server cserver/main.c

FROM alpine:3.20
WORKDIR /app
LABEL org.opencontainers.image.source="https://github.com/renatograsso10/rinha-backend-2026-go"
COPY --from=build /out/server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
