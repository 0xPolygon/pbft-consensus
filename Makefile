
test:
	go test -v ./...

e2e:
	cd ./e2e && go test -v ./...

.PHONY: test e2e
