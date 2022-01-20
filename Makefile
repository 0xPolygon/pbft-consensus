
test:
	go test -v ./...

e2e:
	cd ./e2e && go test -v ./...

fuzz:
	cd ./e2e && go test -run TestFuzz


.PHONY: test e2e
