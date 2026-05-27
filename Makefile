.PHONY: build build-local docker-build docker-run clean

APP_NAME ?= go-gateway
IMAGE_NAME ?= ai-web-gateway
IMAGE_TAG ?= latest

# Build Go binary locally (for testing)
build-local:
	cd /projects/ai-web-gateway && CGO_ENABLED=0 go build -ldflags="-s -w" -o $(APP_NAME) .
	@echo "Binary built: /projects/ai-web-gateway/$(APP_NAME)"

# Build Go binary locally (same as build-local, explicit)
build: build-local

# Build Docker image
docker-build:
	cd /projects/ai-web-gateway && docker build -t local/$(IMAGE_NAME):$(IMAGE_TAG) .
	@echo "Image built: local/$(IMAGE_NAME):$(IMAGE_TAG)"

# Build and run locally (for testing)
docker-run: docker-build
	docker run --rm -it \
		-p 8080:80 -p 8443:443 \
		-v /tmp/ai-gateway-sites:/sites \
		-e PUBLIC_HOST=192.168.254.240:8080 \
		-e LISTEN_ADDR=:9000 \
		-e SITES_DIR=/sites \
		-e BACKEND_PORT_START=10000 \
		-e BACKEND_PORT_END=19999 \
		local/$(IMAGE_NAME):$(IMAGE_TAG)

# Stop and remove the running container
docker-stop:
	docker stop ai-web-gateway || true
	docker rm ai-web-gateway || true

# Clean local build artifacts
clean:
	rm -f /projects/ai-web-gateway/$(APP_NAME)

# Show help
help:
	@echo "Available targets:"
	@echo "  make build-local   - Build Go binary locally"
	@echo "  make docker-build  - Build Docker image"
	@echo "  make docker-run    - Build and run container for testing"
	@echo "  make docker-stop   - Stop running container"
	@echo "  make clean         - Remove build artifacts"
