# Broker image. CGO disabled because modernc.org/sqlite is pure Go.
# Builds only the root package (broker + CLI) — the tui/ package is not included.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /cpv2 .

FROM alpine:3.20
RUN apk add --no-cache curl                 # curl so the same image can drive the harness
COPY --from=build /cpv2 /usr/local/bin/cpv2
ENV CPV2_ADDR=0.0.0.0:7900
EXPOSE 7900
ENTRYPOINT ["cpv2"]
CMD ["serve"]
