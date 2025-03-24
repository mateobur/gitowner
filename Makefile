APP_NAME = topowners
GOPATH ?= ~/go
BIN_DIR = $(GOPATH)/bin

build:
	go build -o $(APP_NAME) .

install: build
	install -m 755 $(APP_NAME) $(BIN_DIR)/$(APP_NAME)
