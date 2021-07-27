FROM golang:1.16-alpine AS build-stage

WORKDIR /go/src/github.com/heilerich/k8s-resource-annotator

RUN apk add git

COPY . .

RUN CGO_ENABLED=0 go build -o /bin/webhook --ldflags "-w -extldflags '-static'" github.com/heilerich/k8s-resource-annotator

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=build-stage /bin/webhook /usr/local/bin/webhook
ENTRYPOINT ["/usr/local/bin/webhook"]
