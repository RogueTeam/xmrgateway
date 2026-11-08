FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /xmrgateway ./cmd/xmrgateway \
    && mkdir /srv && cd /srv && /xmrgateway deploy

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /xmrgateway /xmrgateway
COPY --from=build /srv /srv
WORKDIR /srv
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/xmrgateway"]
CMD ["server", "-config", "/etc/xmrgateway/config.json"]
