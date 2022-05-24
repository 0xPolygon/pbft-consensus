
test:
	go test -v --race -shuffle=on ./...

e2e:
	cd ./e2e && go test -v ./...

fuzz:
	cd ./e2e && go test -run TestFuzz

lintci:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.46.1

lint:
	@"$(GOPATH)/bin/golangci-lint" run --config ./.golangci.yml ./...


.PHONY: test e2e
