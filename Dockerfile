FROM golang:1.26@sha256:eb36c1664dd974cde625f736e02c204383deebe03977365caaec5bf49f794348 AS builder
WORKDIR /workspace
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/thoth-operator ./cmd/manager

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
COPY --from=builder /out/thoth-operator /thoth-operator
USER 65532:65532
ENTRYPOINT ["/thoth-operator"]
