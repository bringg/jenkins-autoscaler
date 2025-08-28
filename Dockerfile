FROM golang:1.25.0-alpine as builder

ARG VERSION=development
ENV VERSION=$VERSION

WORKDIR /opt/jenkins-autoscaler/
COPY . .
RUN apk add --no-cache git make \
    && make install

FROM alpine:3.22
LABEL maintainer "Bringg DevOps <devops@bringg.com>"

ENV JAS_CONFIG=/dev/null

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /go/bin/jas /usr/local/bin

USER nobody

EXPOSE 8080
ENTRYPOINT ["jas"]
