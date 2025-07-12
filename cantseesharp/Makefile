CC = gcc
CFLAGS = -O3 -march=native -flto -ffast-math -funroll-loops -finline-functions -DAVX2_ENABLED -std=c11 -D_GNU_SOURCE -Wall -Wextra
LDFLAGS = -lpthread -luring
TARGET = shadow_engine
SOURCE = shadow_engine.c

# TAPEWORM Domain Processing Engine

.PHONY: all clean benchmark run_test

all: $(TARGET)

$(TARGET): $(SOURCE)
	$(CC) $(CFLAGS) -o $(TARGET) $(SOURCE) $(LDFLAGS)

clean:
	rm -f $(TARGET) *.o

run_test: $(TARGET)
	@echo "Testing TAPEWORM with tiny_test.txt..."
	@time ./$(TARGET) -live ../tests/tiny_test.txt

benchmark: $(TARGET)
	@echo "=== TAPEWORM Domain Processing Benchmark ==="
	@echo "Testing C implementation:"
	@time ./$(TARGET) -live ../tests/benchmark_domains.txt > /dev/null 2>&1 || echo "No benchmark_domains.txt found"
	@echo
	@echo "Testing Go main_advanced.go:"
	@time cd .. && go run main_advanced.go -live tests/tiny_test.txt > /dev/null 2>&1 || echo "Go main_advanced.go failed"
	@echo
	@echo "Testing Go main_extreme.go:"
	@time cd .. && go run main_extreme.go -live tests/tiny_test.txt > /dev/null 2>&1 || echo "Go main_extreme.go failed"