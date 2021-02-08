IMAGE := natsio/nats-surveyor

TAG := 0.2.0

build:
	$(info Make: Building "$(TAG)" tagged images.)
	@docker build -t $(IMAGE):$(TAG) .
	@docker tag $(IMAGE):$(TAG) $(IMAGE):latest

push:
	$(info Make: Pushing "$(TAG)" tagged image.)
	@docker push $(IMAGE):$(TAG)
	@docker push $(IMAGE):latest
