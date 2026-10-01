# API TGVmax TrainNomad (Go). Image finale ~10 Mo : le binaire + network.bin.gz (repli si la Release GitHub
# est injoignable au démarrage ; sinon le réseau est téléchargé et rechargé à chaud, voir reload.go).
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api .

FROM scratch
COPY --from=build /out/api /api
# Certificats racine : indispensables pour télécharger le réseau en HTTPS (l'image scratch n'en a pas).
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY network.bin.gz /network.bin.gz
# Render fournit PORT (10000 par défaut) ; la valeur ci-dessous ne sert qu'en local.
ENV NETWORK_PATH=/network.bin.gz GOMAXPROCS=1 PORT=10000
EXPOSE 10000
ENTRYPOINT ["/api"]
