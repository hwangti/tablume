# Tablume browser build (passkey sign-in). See README "웹 버전".
#
#   docker build -t tablume .
#   docker run -d --name tablume -p 127.0.0.1:8080:8080 -v tablume:/data \
#     -e TABLUME_URL=https://db.example.com tablume
#   docker logs tablume      # the first passkey's setup link

FROM node:22-alpine AS ui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS app
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -tags server -trimpath -ldflags "-s -w" -o /tablume .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -h /data tablume
COPY --from=app /tablume /usr/local/bin/tablume
USER tablume
ENV TABLUME_LISTEN=:8080 \
    TABLUME_DATA=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["tablume"]
