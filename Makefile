.PHONY: up down reset logs ps test test-local demo psql redis-cli

up:            ## Build và chạy API + PostgreSQL + Redis
	docker compose up -d --build
	@echo "API: http://localhost:$${API_PORT:-8080}  (chạy 'make demo' để thử toàn bộ luồng)"

down:          ## Dừng container, giữ dữ liệu
	docker compose down

reset:         ## Dừng và xoá sạch dữ liệu
	docker compose down -v

logs:
	docker compose logs -f api

ps:
	docker compose ps

test:          ## Chạy toàn bộ test (có race detector) bên trong Docker
	DOCKER_BUILDKIT=1 docker build --target test -t youpass-share:test .

test-local:    ## Chạy test bằng Go trên máy
	go test -race -count=1 ./...

demo:          ## Demo end-to-end bằng curl
	./scripts/demo.sh

psql:
	docker compose exec postgres psql -U youpass -d youpass

redis-cli:
	docker compose exec redis redis-cli
