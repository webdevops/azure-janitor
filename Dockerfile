FROM golang:1.17-alpine as build

RUN apk upgrade --no-cache --force
RUN apk add --update build-base make git

WORKDIR /go/src/github.com/webdevops/azure-janitor

# Compile
COPY ./ /go/src/github.com/webdevops/azure-janitor
RUN go mod download
RUN make test
RUN make build
RUN ./azure-janitor --help

#############################################
# FINAL IMAGE
#############################################
FROM gcr.io/distroless/static
ENV LOG_JSON=1
COPY --from=build /go/src/github.com/webdevops/azure-janitor/azure-janitor /
USER 1000:1000
ENTRYPOINT ["/azure-janitor"]
