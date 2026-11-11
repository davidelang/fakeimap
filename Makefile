.PHONY: all clean

# go test needs a writable build cache. This sandbox cannot create the
# default cache under $$HOME/.cache, so fall back to /tmp when it cannot.
all:
	if mkdir -p "$$(go env GOCACHE)" 2>/dev/null; then \
		go test ./... && go build -o fakeimap .; \
	else \
		mkdir -p /tmp/fakeimap-gocache && \
		GOCACHE=/tmp/fakeimap-gocache go test ./... && \
		GOCACHE=/tmp/fakeimap-gocache go build -o fakeimap .; \
	fi

clean:
	rm -f fakeimap
