FROM golang:1.19-alpine
RUN apk update && apk --no-cache --update add build-base
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o /bin/app 

ENTRYPOINT [ "/bin/app" ]