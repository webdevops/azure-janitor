FROM golang:1.13 as build

WORKDIR /go/src/github.com/webdevops/azure-janitor

# Get deps (cached)
COPY ./go.mod /go/src/github.com/webdevops/azure-janitor
COPY ./go.sum /go/src/github.com/webdevops/azure-janitor
RUN go mod download

# Compile
COPY ./ /go/src/github.com/webdevops/azure-janitor
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o /azure-janitor \
    && chmod +x /azure-janitor
RUN /azure-janitor --help

#############################################
# FINAL IMAGE
#############################################
FROM gcr.io/distroless/static
COPY --from=build /azure-janitor /
USER 1000
ENTRYPOINT ["/azure-janitor"]
