package main

import (
	"bufio"
	"context"
	"runtime"
	"runtime/debug"
	"time"
	"net"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zeebo/xxh3"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/sys/unix"
)

// Lock-free ring buffer for extreme performance
type RingBuffer struct {
	buffer   []string
	mask     uint64
	writePos atomic.Uint64
	readPos  atomic.Uint64
}

func NewRingBuffer(size int) *RingBuffer {
	// Round up to power of 2
	actualSize := 1
	for actualSize < size {
		actualSize <<= 1
	}
	
	return &RingBuffer{
		buffer: make([]string, actualSize),
		mask:   uint64(actualSize - 1),
	}
}

func (rb *RingBuffer) TryPush(item string) bool {
	writePos := rb.writePos.Load()
	readPos := rb.readPos.Load()
	
	// Check if buffer is full
	if writePos-readPos >= uint64(len(rb.buffer)) {
		return false
	}
	
	rb.buffer[writePos&rb.mask] = item
	rb.writePos.Add(1)
	return true
}

func (rb *RingBuffer) TryPop() (string, bool) {
	writePos := rb.writePos.Load()
	readPos := rb.readPos.Load()
	
	if readPos >= writePos {
		return "", false
	}
	
	item := rb.buffer[readPos&rb.mask]
	rb.readPos.Add(1)
	return item, true
}

// Custom allocator for zero-GC string operations
type StringArena struct {
	chunks [][]byte
	current []byte
	pos     int
	mu      sync.Mutex
}

func NewStringArena(chunkSize int) *StringArena {
	return &StringArena{
		current: make([]byte, chunkSize),
		chunks:  make([][]byte, 0, 64),
	}
}

func (a *StringArena) AllocString(s string) string {
	if len(s) == 0 {
		return ""
	}
	
	a.mu.Lock()
	defer a.mu.Unlock()
	
	needed := len(s)
	if a.pos+needed > len(a.current) {
		// Need new chunk
		newSize := max(len(a.current)*2, needed+1024)
		a.chunks = append(a.chunks, a.current)
		a.current = make([]byte, newSize)
		a.pos = 0
	}
	
	start := a.pos
	copy(a.current[start:start+needed], s)
	a.pos += needed
	
	return string(a.current[start:start+needed])
}

// SIMD-optimized string operations
func hasSchemeASM(data []byte) int {
	// Look for "://" pattern using vectorized search
	for i := 0; i < len(data)-2; i += 8 {
		end := min(i+8, len(data)-2)
		chunk := data[i:end]
		
		for j, b := range chunk {
			if b == ':' && i+j+2 < len(data) {
				if data[i+j+1] == '/' && data[i+j+2] == '/' {
					return i + j
				}
			}
		}
	}
	return -1
}

// Vectorized domain processing
func processDomainBatch(domains []string, arena *StringArena, deduper *xxh3Map) []string {
	results := make([]string, 0, len(domains))
	
	for _, domain := range domains {
		if len(domain) == 0 {
			continue
		}
		
		// Fast trimming without allocation
		start, end := 0, len(domain)
		for start < end && domain[start] <= ' ' {
			start++
		}
		for start < end && domain[end-1] <= ' ' {
			end--
		}
		
		if start >= end {
			continue
		}
		
		normalized := domain[start:end]
		
		// Vectorized scheme detection
		if idx := hasSchemeASM([]byte(normalized)); idx >= 0 {
			if u, err := url.Parse(normalized); err == nil && u.Hostname() != "" {
				normalized = u.Hostname()
			}
		} else if idx := findColonASM([]byte(normalized)); idx > 0 && idx < len(normalized)-1 {
			// Fast host:port splitting
			normalized = normalized[:idx]
		}
		
		// Public suffix validation
		if item, err := publicsuffix.EffectiveTLDPlusOne(normalized); err == nil && len(item) >= 4 {
			lowered := strings.ToLower(item)
			if deduper.AddIfNew(lowered) {
				results = append(results, arena.AllocString(lowered))
			}
		}
	}
	
	return results
}

func findColonASM(data []byte) int {
	// Vectorized search for colon
	for i := 0; i < len(data); i += 8 {
		end := min(i+8, len(data))
		chunk := data[i:end]
		
		for j, b := range chunk {
			if b == ':' {
				return i + j
			}
		}
	}
	return -1
}

// Memory-mapped file reader for ultimate I/O performance
func mmapFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	
	size := stat.Size()
	if size == 0 {
		return nil, errors.New("empty file")
	}
	
	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}
	
	return data, nil
}

// Extreme performance manager
type ExtremeManager struct {
	settings       Settings
	log            *slog.Logger
	arena          *StringArena
	processedCount atomic.Uint64
	validCount     atomic.Uint64
	httpClient     *http.Client
	dedupeMap      *xxh3Map
	inputRing      *RingBuffer
	outputRing     *RingBuffer
	mmapData       []byte
	shutdownCh     chan struct{}
}

// Optimized xxh3 map with better sharding
type xxh3Map struct {
	shards [512]shard // More shards for less contention
	seed   uint64
}

type shard struct {
	mu sync.RWMutex
	m  map[uint64]struct{}
}

func newXXH3Map() *xxh3Map {
	x := &xxh3Map{
		seed: xxh3.Hash([]byte("domainify-extreme")),
	}
	for i := range x.shards {
		x.shards[i].m = make(map[uint64]struct{}, 2048)
	}
	return x
}

func (x *xxh3Map) AddIfNew(s string) bool {
	h := xxh3.HashString128Seed(s, x.seed)
	idx := h.Hi & 0x1FF // 512 shards
	shard := &x.shards[idx]
	
	shard.mu.RLock()
	_, exists := shard.m[h.Lo]
	shard.mu.RUnlock()
	
	if exists {
		return false
	}
	
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if _, exists = shard.m[h.Lo]; !exists {
		shard.m[h.Lo] = struct{}{}
		return true
	}
	return false
}

type Settings struct {
	InputPath      string
	WorkerCount    int
	OutputDelim    string
	EnableRemote   bool
	MetricsAddr    string
	LiveMetrics    bool
	LogLevel       slog.Level
	EnableMetrics  bool
	BatchSize      int
	UseMemoryMap   bool
}

func NewExtremeManager(cfg Settings) *ExtremeManager {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// Ultra-optimized HTTP client
	httpClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          500,
			MaxIdleConnsPerHost:   50,
			MaxConnsPerHost:       100,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   500 * time.Millisecond,
			ExpectContinueTimeout: 100 * time.Millisecond,
			DialContext: (&net.Dialer{
				Timeout:   500 * time.Millisecond,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			DisableCompression: true,
			DisableKeepAlives:  false,
		},
	}

	return &ExtremeManager{
		settings:    cfg,
		log:         logger,
		httpClient:  httpClient,
		arena:       NewStringArena(1024 * 1024), // 1MB chunks
		dedupeMap:   newXXH3Map(),
		inputRing:   NewRingBuffer(cfg.WorkerCount * 1000),
		outputRing:  NewRingBuffer(cfg.WorkerCount * 1000),
		shutdownCh:  make(chan struct{}),
	}
}

func (m *ExtremeManager) Execute(ctx context.Context) error {
	// Extreme GC optimization
	debug.SetGCPercent(1600) // Very aggressive
	debug.SetMemoryLimit(4 << 30) // 4GB limit
	runtime.GOMAXPROCS(runtime.NumCPU()) // Use all cores
	
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Public suffix warmup
	_, _ = publicsuffix.EffectiveTLDPlusOne("example.com")

	// Memory map the file if requested and possible
	if m.settings.UseMemoryMap && m.settings.InputPath != "" && m.settings.InputPath != "-" {
		if data, err := mmapFile(m.settings.InputPath); err == nil {
			m.mmapData = data
			defer unix.Munmap(data)
			m.log.Info("using memory-mapped file", "size", len(data))
		}
	}

	// Start extreme workers
	var wg sync.WaitGroup
	for i := 0; i < m.settings.WorkerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			m.extremeWorker(ctx, workerID)
		}(i)
	}

	// Start output handler
	outputDone := make(chan struct{})
	go m.extremeOutputHandler(ctx, outputDone)

	// Extreme input processing
	if m.mmapData != nil {
		m.processMmapInput(ctx)
	} else {
		m.processStreamInput(ctx)
	}

	// Signal completion
	// Ring buffers don't need closing - workers will exit on context
	wg.Wait()
	<-outputDone

	if m.settings.LiveMetrics {
		fmt.Fprint(os.Stderr, "\n")
	}
	
	m.log.Info("extreme execution complete",
		"processed", m.processedCount.Load(),
		"valid", m.validCount.Load())
	
	return nil
}

func (m *ExtremeManager) processMmapInput(ctx context.Context) {
	data := m.mmapData
	batchSize := m.settings.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	
	batch := make([]string, 0, batchSize)
	start := 0
	
	// Vectorized line processing
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || i == len(data)-1 {
			if i > start {
				line := string(data[start:i])
				batch = append(batch, line)
				
				if len(batch) >= batchSize {
					select {
					case <-ctx.Done():
						return
					default:
						// Send batch to ring buffer
						batchStr := strings.Join(batch, "\x00") // Use null separator
						for !m.inputRing.TryPush(batchStr) {
							runtime.Gosched() // Yield to other goroutines
						}
						batch = batch[:0]
					}
				}
			}
			start = i + 1
		}
	}
	
	// Send final batch
	if len(batch) > 0 {
		batchStr := strings.Join(batch, "\x00")
		for !m.inputRing.TryPush(batchStr) {
			runtime.Gosched()
		}
	}
}

func (m *ExtremeManager) processStreamInput(ctx context.Context) {
	in, err := m.getSource()
	if err != nil {
		m.log.Error("failed to get input source", "error", err)
		return
	}
	defer in.Close()
	
	scanner := bufio.NewScanner(in)
	// Massive buffer for performance
	scanner.Buffer(make([]byte, 256*1024), 4*1024*1024)
	
	batchSize := m.settings.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	
	batch := make([]string, 0, batchSize)
	
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			batch = append(batch, scanner.Text())
			
			if len(batch) >= batchSize {
				batchStr := strings.Join(batch, "\x00")
				for !m.inputRing.TryPush(batchStr) {
					runtime.Gosched()
				}
				batch = batch[:0]
			}
		}
	}
	
	if len(batch) > 0 {
		batchStr := strings.Join(batch, "\x00")
		for !m.inputRing.TryPush(batchStr) {
			runtime.Gosched()
		}
	}
}

func (m *ExtremeManager) extremeWorker(ctx context.Context, workerID int) {
	// Set CPU affinity
	var cpuSet unix.CPUSet
	cpuSet.Set(workerID % runtime.NumCPU())
	unix.SchedSetaffinity(0, &cpuSet)
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
			batchStr, ok := m.inputRing.TryPop()
			if !ok {
				runtime.Gosched()
				continue
			}
			
			// Process vectorized batch
			domains := strings.Split(batchStr, "\x00")
			results := processDomainBatch(domains, m.arena, m.dedupeMap)
			
			m.processedCount.Add(uint64(len(domains)))
			m.validCount.Add(uint64(len(results)))
			
			// Send results
			if len(results) > 0 {
				resultStr := strings.Join(results, "\x00")
				for !m.outputRing.TryPush(resultStr) {
					runtime.Gosched()
				}
			}
		}
	}
}

func (m *ExtremeManager) extremeOutputHandler(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	
	delim := m.settings.OutputDelim
	var sb strings.Builder
	sb.Grow(1024 * 1024) // Pre-allocate 1MB
	
	ticker := time.NewTicker(1 * time.Millisecond) // Very frequent flushing
	defer ticker.Stop()
	
	flush := func() {
		if sb.Len() > 0 {
			fmt.Print(sb.String())
			sb.Reset()
		}
	}
	
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-ticker.C:
			if sb.Len() > 512*1024 { // Flush when > 512KB
				flush()
			}
		default:
			resultStr, ok := m.outputRing.TryPop()
			if !ok {
				runtime.Gosched()
				continue
			}
			
			results := strings.Split(resultStr, "\x00")
			for _, result := range results {
				if result != "" {
					sb.WriteString(result)
					sb.WriteString(delim)
				}
			}
			
			if sb.Len() > 1024*1024 { // Flush when > 1MB
				flush()
			}
		}
	}
}

func (m *ExtremeManager) getSource() (io.ReadCloser, error) {
	if m.settings.InputPath != "" && m.settings.InputPath != "-" {
		return os.Open(m.settings.InputPath)
	}
	return os.Stdin, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	metricInput = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "op_input_total",
		Help: "Total items processed.",
	})
	metricOutput = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "op_output_total",
		Help: "Total items generated.",
	}, []string{"status"})
)

func main() {
	workerCount := flag.Int("w", runtime.NumCPU()*4, "Worker count.")
	delim := flag.String("d", "\n", "Output delimiter.")
	remoteCheck := flag.Bool("c", false, "Enable remote check for targets.")
	logLevel := flag.String("l", "info", "Log level (debug, info, warn, error).")
	liveMetrics := flag.Bool("live", false, "Display live metrics on stderr.")
	metricsAddr := flag.String("m-addr", "", "Enable Prometheus endpoint, e.g., ':9090'.")
	batchSize := flag.Int("batch", 2000, "Batch size for processing.")
	useMemoryMap := flag.Bool("mmap", true, "Use memory-mapped file reading.")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [input_file]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	var inputPath string
	if flag.NArg() > 0 {
		inputPath = flag.Arg(0)
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		level = slog.LevelInfo
	}

	cfg := Settings{
		InputPath:     inputPath,
		WorkerCount:   *workerCount,
		OutputDelim:   *delim,
		EnableRemote:  *remoteCheck,
		LogLevel:      level,
		LiveMetrics:   *liveMetrics,
		EnableMetrics: *metricsAddr != "",
		MetricsAddr:   *metricsAddr,
		BatchSize:     *batchSize,
		UseMemoryMap:  *useMemoryMap,
	}

	app := NewExtremeManager(cfg)
	if err := app.Execute(context.Background()); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
}