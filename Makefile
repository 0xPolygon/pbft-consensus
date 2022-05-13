
test:
	go test -v --race ./...

e2e:
	cd ./e2e && go test -p 1 --race --shuffle=on -v ./...

fuzz:
	cd ./e2e && go test -run TestFuzz


.PHONY: test e2e
