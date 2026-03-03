PI_HOST  ?= pi@raspberrypi.local
DEPLOY_DIR = /opt/rssbridge
BINARY   = rssBridge

.PHONY: build build-pi deploy

build:
	go build -o $(BINARY) .

build-pi:
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-linux-arm64 .

deploy: build-pi
	ssh $(PI_HOST) "sudo mkdir -p $(DEPLOY_DIR)"
	scp $(BINARY)-linux-arm64 $(PI_HOST):/tmp/$(BINARY)
	scp -r templates $(PI_HOST):/tmp/rssbridge-templates
	ssh $(PI_HOST) "\
		sudo mv /tmp/$(BINARY) $(DEPLOY_DIR)/$(BINARY) && \
		sudo chmod +x $(DEPLOY_DIR)/$(BINARY) && \
		sudo rm -rf $(DEPLOY_DIR)/templates && \
		sudo mv /tmp/rssbridge-templates $(DEPLOY_DIR)/templates && \
		sudo chown -R rssbridge:rssbridge $(DEPLOY_DIR) 2>/dev/null || true"
	@echo "Deploy done. Run 'make install-service' to set up systemd."

install-service:
	scp deploy/rssbridge.service $(PI_HOST):/tmp/rssbridge.service
	ssh $(PI_HOST) "\
		sudo mv /tmp/rssbridge.service /etc/systemd/system/rssbridge.service && \
		sudo systemctl daemon-reload && \
		sudo systemctl enable rssbridge && \
		sudo systemctl restart rssbridge && \
		sudo systemctl status rssbridge"
