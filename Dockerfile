# syntax=docker/dockerfile:1
FROM golang:1.20-bullseye AS build

ENV GOPATH /go
ENV GOPROXY=https://proxy.golang.org,direct
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
ENV CGO_ENABLED=0

WORKDIR /workdir

COPY go.mod /workdir/go.mod
COPY go.sum /workdir/go.sum

RUN go mod download

COPY . /workdir

RUN go build -ldflags '-extldflags "-static -lm"' -tags 'osusergo netgo static_build' -o snider .

# Execution container
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /workdir/snider /snider

ENTRYPOINT ["/snider"]
