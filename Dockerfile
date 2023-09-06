FROM golang:1.20.7-alpine3.18 as builder

# install upx dep
RUN apk add --no-cache \
    binutils \
    ca-certificates \
    curl \
    git \
    tzdata
#  && go get -u github.com/golang/dep/...

# setup the working directory
WORKDIR /go/src/github.com/bluexlab/kube-grpc-balancer

# add source code
ADD . .
RUN go mod download

# build the binary
RUN CGO_ENABLED=0 GOOS=`go env GOHOSTOS` GOARCH=`go env GOHOSTARCH` \
    go build -a -installsuffix cgo -o /go/bin/gproxy ./cmd/gproxy

# FROM scratch
FROM alpine:3.18

ENV PATH /app:$PATH

WORKDIR /app
COPY --from=builder /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/zoneinfo.zip
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/bin/gproxy /app/gproxy

# add launch shell command
COPY docker-entrypoint.sh /usr/bin/

ENTRYPOINT ["docker-entrypoint.sh"]
