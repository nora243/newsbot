FROM golang:1.24.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /newsbot ./cmd/newsbot

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg fonts-noto-core && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home newsbot && mkdir -p /app/data/videos && chown -R newsbot:newsbot /app
WORKDIR /app
COPY --from=build /newsbot /usr/local/bin/newsbot
COPY migrations ./migrations
USER newsbot
ENTRYPOINT ["newsbot"]
CMD ["api"]
