############################################################################################################
# BUILD
############################################################################################################

FROM golang:1.21.0-alpine as builder

ARG VERSION=development
ENV VERSION=$VERSION

WORKDIR /opt/jenkins-autoscaler

COPY . .

RUN apk add --no-cache git make && make install

############################################################################################################
# RELEASE
############################################################################################################

FROM alpine:3.18

LABEL maintainer "Bringg DevOps <devops@bringg.com>"

ENV JAS_CONFIG=/opt/jenkins-autoscaler/config.yml

RUN apk add --no-cache ca-certificates tzdata

# Envplate to dynamically change configurations
RUN wget -q https://github.com/kreuzwerker/envplate/releases/download/v1.0.2/envplate_1.0.2_$(uname -s)_$(uname -m).tar.gz -O - \
    | tar xz \
    && mv envplate /usr/local/bin/ep \
    && chmod +x /usr/local/bin/ep

WORKDIR /opt/jenkins-autoscaler

COPY --from=builder /go/bin/jas /usr/local/bin
COPY --from=builder --chown=nobody:nobody /opt/jenkins-autoscaler/docker.config.yml /opt/jenkins-autoscaler/config.yml

USER nobody

EXPOSE 8080

ENTRYPOINT [ "sh", "-c", "/usr/local/bin/ep /opt/jenkins-autoscaler/config.yml && /usr/local/bin/jas --backend=${BACKEND}" ]

############################################################################################################