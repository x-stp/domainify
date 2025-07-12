#!/bin/bash

# Comprehensive benchmark script for domain processing implementations

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Domainify Implementation Benchmark ===${NC}\n"

# Check if test files exist
if [ ! -f "../tests/tiny_test.txt" ]; then
    echo -e "${RED}Error: Test files not found${NC}"
    exit 1
fi

# Build C implementation
echo -e "${YELLOW}Building C implementation...${NC}"
if ! make; then
    echo -e "${RED}Failed to build C implementation${NC}"
    exit 1
fi

# Function to run benchmark and extract metrics
run_benchmark() {
    local impl="$1"
    local cmd="$2"
    local testfile="$3"
    
    echo -e "\n${GREEN}Testing: $impl${NC}"
    echo -e "${BLUE}Command: $cmd${NC}"
    
    # Run with timing and capture both stdout and stderr
    local start_time=$(date +%s.%N)
    
    # Capture output to temp files
    local out_file=$(mktemp)
    local err_file=$(mktemp)
    
    if eval "$cmd" > "$out_file" 2> "$err_file"; then
        local end_time=$(date +%s.%N)
        local runtime=$(echo "$end_time - $start_time" | bc -l)
        
        # Count output lines (unique domains)
        local unique_count=$(wc -l < "$out_file")
        
        # Extract processing stats from stderr if available
        local processed_line=$(grep -E "(Processed|processed)" "$err_file" | tail -1 || echo "")
        
        printf "${GREEN}✓ Success${NC}\n"
        printf "  Runtime: %.3f seconds\n" "$runtime"
        printf "  Unique domains output: %d\n" "$unique_count"
        
        if [ -n "$processed_line" ]; then
            echo "  Stats: $processed_line"
        fi
        
        # Calculate rate
        if [ "$unique_count" -gt 0 ] && [ "$(echo "$runtime > 0" | bc -l)" -eq 1 ]; then
            local rate=$(echo "scale=0; $unique_count / $runtime" | bc -l)
            printf "  Rate: %.0f domains/sec\n" "$rate"
        fi
    else
        echo -e "${RED}✗ Failed${NC}"
        echo "Error output:"
        cat "$err_file" | head -5
    fi
    
    # Cleanup temp files
    rm -f "$out_file" "$err_file"
}

# Test with different file sizes
for testfile in tiny_test.txt small_test.txt medium_test.txt; do
    testpath="../tests/$testfile"
    if [ -f "$testpath" ]; then
        echo -e "\n${YELLOW}=== Testing with $testfile ===${NC}"
        
        # Count input lines
        input_lines=$(wc -l < "$testpath")
        echo -e "Input lines: $input_lines\n"
        
        # Test C implementation (ultra-blazing)
        run_benchmark "C Ultra-Blazing" "./test -live $testpath" "$testpath"
        
        # Test Go main.go
        run_benchmark "Go Basic" "go run ../main.go -live $testpath" "$testpath"
        
        # Test Go main_advanced.go if it exists
        if [ -f "../main_advanced.go" ]; then
            run_benchmark "Go Advanced" "go run ../main_advanced.go -live $testpath" "$testpath"
        fi
        
        echo -e "\n${BLUE}----------------------------------------${NC}"
    else
        echo -e "${YELLOW}Skipping $testfile (not found)${NC}"
    fi
done

# Memory usage comparison (if available)
echo -e "\n${YELLOW}=== Memory Usage Analysis ===${NC}"

if command -v valgrind >/dev/null 2>&1; then
    echo "Running memory analysis with Valgrind..."
    valgrind --tool=massif --pages-as-heap=yes ./test tiny_test.txt > /dev/null 2>&1 || true
    if [ -f "massif.out.*" ]; then
        echo "Memory profile generated: massif.out.*"
        echo "Use 'ms_print massif.out.*' to view detailed memory usage"
    fi
else
    echo "Valgrind not available for memory analysis"
fi

# CPU profiling hint
echo -e "\n${YELLOW}=== Profiling Tips ===${NC}"
echo "For CPU profiling:"
echo "  perf record -g ./test benchmark_domains.txt > /dev/null"
echo "  perf report"
echo ""
echo "For Go profiling:"
echo "  go run -cpuprofile=cpu.prof ../main_advanced.go benchmark_domains.txt > /dev/null"
echo "  go tool pprof cpu.prof"

echo -e "\n${GREEN}Benchmark complete!${NC}"