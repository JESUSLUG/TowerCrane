FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o towercrane ./cmd

# OpenTofu binary from the official image.
FROM ghcr.io/opentofu/opentofu:1.9.1 AS tofu

# Terragrunt, verified against the release checksums.
FROM alpine:3.20 AS terragrunt
ARG TERRAGRUNT_VERSION=v1.2.1
RUN apk --no-cache add curl \
 && curl -fsSLO https://github.com/gruntwork-io/terragrunt/releases/download/${TERRAGRUNT_VERSION}/terragrunt_linux_amd64 \
 && curl -fsSLO https://github.com/gruntwork-io/terragrunt/releases/download/${TERRAGRUNT_VERSION}/SHA256SUMS \
 && grep " terragrunt_linux_amd64$" SHA256SUMS | sha256sum -c - \
 && install -m 0755 terragrunt_linux_amd64 /usr/local/bin/terragrunt

FROM alpine:3.20
# git: clones repositories; tofu/terragrunt: run plan/apply. (For
# Terraform instead of OpenTofu, add the terraform binary.)
RUN apk --no-cache add ca-certificates git
COPY --from=tofu /usr/local/bin/tofu /usr/local/bin/tofu
COPY --from=terragrunt /usr/local/bin/terragrunt /usr/local/bin/terragrunt
WORKDIR /app
COPY --from=builder /app/towercrane .

EXPOSE 8080
CMD ["./towercrane"]
