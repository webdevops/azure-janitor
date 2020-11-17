FROM golang:1.14 as build

WORKDIR /go/src/github.com/webdevops/azure-janitor

# Get deps (cached)
COPY ./go.mod /go/src/github.com/webdevops/azure-janitor
COPY ./go.sum /go/src/github.com/webdevops/azure-janitor
RUN go mod download

# Compile
COPY ./ /go/src/github.com/webdevops/azure-janitor
RUN make test
RUN make lint
RUN make build
RUN ./azure-janitor --help

#############################################
# FINAL IMAGE
#############################################
FROM gcr.io/distroless/static
COPY --from=build /go/src/github.com/webdevops/azure-janitor/azure-janitor /
USER 1000
ENTRYPOINT ["/azure-janitor"]
