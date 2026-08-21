FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/crossborder-sim ./cmd/simulator

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
RUN adduser -D -u 10001 app
USER app
COPY --from=builder /out/crossborder-sim /usr/local/bin/crossborder-sim
EXPOSE 8091
ENTRYPOINT ["crossborder-sim"]
