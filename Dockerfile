# Build the manager binary
FROM golang:1.24 AS builder

ARG GH_TOKEN

ARG PRIVATE_REPO_HOST=github.com/scality

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

RUN go env -w GOPRIVATE=${PRIVATE_REPO_HOST}

RUN if [ -z "$GH_TOKEN" ]; then echo "GH_TOKEN is missing"; exit 1; fi && \
    git config --global url."https://oauth2:${GH_TOKEN}@${PRIVATE_REPO_HOST}".insteadOf "https://${PRIVATE_REPO_HOST}"

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Propagate SOURCE_DATE_EPOCH to the build environment, for reproducible builds
ARG SOURCE_DATE_EPOCH

# Allow passing version information
ARG VERSION=dev

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager \
    -ldflags="-X main.version=${VERSION}" \
    cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
