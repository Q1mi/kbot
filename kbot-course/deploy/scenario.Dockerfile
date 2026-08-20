FROM golang:1.26.6-alpine AS builder

ARG PROJECT
WORKDIR /src
COPY projects/${PROJECT}/go.mod ./
RUN go mod download
COPY projects/${PROJECT}/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/simulator ./cmd/simulator

FROM alpine:3.23
COPY --from=builder /out/simulator /usr/local/bin/simulator
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/simulator"]
