FROM golang:1.26@sha256:2981696eed011d747340d7252620932677929cce7d2d539602f56a8d7e9b660b AS builder
WORKDIR /workspace
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/thoth-operator ./cmd/manager

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /out/thoth-operator /thoth-operator
USER 65532:65532
ENTRYPOINT ["/thoth-operator"]
