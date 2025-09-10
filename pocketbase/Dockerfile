FROM golang:1.24

ARG ADMIN_EMAIL
ARG ADMIN_PASSWORD

RUN go install github.com/air-verse/air@latestk

WORKDIR /app

COPY . .

RUN go mod init pb-server
RUN go mod tidy
RUN go run . superuser upsert $ADMIN_EMAIL $ADMIN_PASSWORD

# CMD ["go", "run", ".", "serve", "--http", "0.0.0.0:5000"]
CMD ["air"]
