.PHONY: build run test docker-up docker-down

build:
	go build -o bin/api cmd/api/main.go

run:
	go run cmd/api/main.go

test:
	go test -v ./...

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

docker-scale:
	docker-compose up -d --scale api=3

logs:
	docker-compose logs -f api

migrate:
	go run cmd/api/main.go migrate
