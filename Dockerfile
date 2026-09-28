FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hockeypool .

FROM gcr.io/distroless/static-debian12
COPY --from=build /hockeypool /hockeypool
ENV DATA_DIR=/data PORT=8080
EXPOSE 8080
ENTRYPOINT ["/hockeypool"]
