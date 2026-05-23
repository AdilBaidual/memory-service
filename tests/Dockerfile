FROM golang:1.23-alpine

WORKDIR /tests

# Copy the test module
COPY tests/go.mod tests/go.sum ./
RUN go mod download

# Copy test source and fixtures
COPY tests/*.go ./
COPY tests/*.sh ./
COPY fixtures/ /fixtures/

RUN chmod +x *.sh

# SERVICE_URL is injected at runtime via docker-compose.test.yml
ENV SERVICE_URL=http://memory-service:8080

# Default: run component tests (contract + malformed + concurrent)
# Override CMD in docker-compose.test.yml for fixture tests
CMD ["go", "test", "-v", "-timeout", "120s", \
     "-run", "Test", \
     "."]
