
test:
	go test -v --race -shuffle=on -coverprofile=coverage.out -covermode=atomic ./...

e2e:
	cd ./e2e && go test -v -run TestE2E

property-tests:
	cd ./e2e && go test -v -run TestProperty

fuzz:
	cd ./e2e && go test -timeout=20m -run TestFuzz

lintci:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.46.1

lint:
	@"$(GOPATH)/bin/golangci-lint" run --config ./.golangci.yml ./...


.PHONY: test e2e property-tests
