
test:
	go test -v ./...

e2e:
	cd ./e2e && go test -timeout=50m -v ./...

.PHONY: test e2e
