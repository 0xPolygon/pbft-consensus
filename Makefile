
test:
	go test $(TESTARGS) ./...

e2e:
	cd ./e2e && go test $(TESTARGS) ./...

.PHONY: test e2e
