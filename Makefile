build: pkg/generated
	go build

pkg/generated:
	go generate

run: build
	kubectl apply -f manifests/crd.yaml
	kubectl apply -f manifests/example-foo.yaml
	./wrangler-sample
