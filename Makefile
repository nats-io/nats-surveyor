drepo ?= snappcloud

.PHONY: dockerx test
dockerx:
ifneq ($(ver),)
	# Ensure 'docker buildx ls' shows correct platforms.
	docker buildx build \
		--tag $(drepo)/nats-surveyor:$(ver) --tag $(drepo)/nats-surveyor:latest \
		--platform linux/amd64,linux/arm/v6,linux/arm/v7,linux/arm64/v8 \
		--push .
else
	# Missing version, try this.
	# make dockerx ver=1.2.3
	exit 1
endif

test:
	go test -v -race -p 1 ./...
