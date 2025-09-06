FROM golang:1.24

RUN go install github.com/air-verse/air@latest

WORKDIR /app

COPY . .

RUN go mod init pb-server
RUN go mod tidy
RUN go run . superuser upsert ambareesh.1510@gmail.com mypassword

# CMD ["go", "run", ".", "serve", "--http", "0.0.0.0:5000"]
CMD ["air"]
