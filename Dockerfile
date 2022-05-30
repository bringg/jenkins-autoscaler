FROM golang:1.18-alpine as builder

ARG SOURCE_BRANCH=development
ENV VERSION=$SOURCE_BRANCH

WORKDIR /opt/jenkins-autoscaler/
COPY . .
RUN apk add --no-cache git make \
    && make install

FROM alpine:3.15
LABEL maintainer "Bringg DevOps <devops@bringg.com>"

ENV JAS_CONFIG=/dev/null

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /go/bin/jas /usr/local/bin

USER nobody

EXPOSE 8080
ENTRYPOINT ["jas"]
