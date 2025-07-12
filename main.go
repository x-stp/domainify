package main

import (
	"bufio"
	"context"
	"time"
	"encoding/json"
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
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/publicsuffix"
)

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

type Settings struct {
	InputPath     string
	WorkerCount   int
	OutputDelim   string
	EnableRemote  bool
	MetricsAddr   string
	LiveMetrics   bool
	LogLevel      slog.Level
	EnableMetrics bool
}

type Manager struct {
	settings       Settings
	log            *slog.Logger
	pool           *sync.Pool
	processedCount atomic.Int64
	validCount     atomic.Int64
	failedCount    atomic.Int64
}

func NewManager(cfg Settings) *Manager {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	return &Manager{
		settings: cfg,
		log:      logger,
		pool: &sync.Pool{
			New: func() any {
				b := make([]byte, 0, 4096)
				return &b
			},
		},
	}
}

func (m *Manager) Execute(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initial setup
	_, err := publicsuffix.EffectiveTLDPlusOne("example.com")
	if err != nil {
		return fmt.Errorf("could not init component: %w", err)
	}

	// Optional components
	if m.settings.EnableMetrics && m.settings.MetricsAddr != "" {
		prometheus.MustRegister(metricInput, metricOutput)
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			m.log.Info("starting metrics endpoint", "addr", m.settings.MetricsAddr)
			if err := http.ListenAndServe(m.settings.MetricsAddr, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
				m.log.Error("metrics endpoint failed", "error", err)
			}
		}()
	}

	if m.settings.LiveMetrics {
		go m.statsPrinter(ctx)
	}

	// --- Pipeline ---
	dataCh := make(chan string, m.settings.WorkerCount*100)
	outputCh := make(chan string, m.settings.WorkerCount*100)
	var wg sync.WaitGroup

	for i := 0; i < m.settings.WorkerCount; i++ {
		wg.Add(1)
		go m.processItem(ctx, &wg, i, dataCh, outputCh)
	}

	go func() {
		wg.Wait()
		close(outputCh)
	}()

	handlerDone := make(chan struct{})
	go m.resultHandler(ctx, outputCh, handlerDone)

	// --- Input Stage ---
	in, err := m.getSource(m.settings.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	scanner := bufio.NewScanner(in)
InputLoop:
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			m.log.Warn("context cancelled, terminating input")
			break InputLoop
		default:
			dataCh <- scanner.Text()
		}
	}
	close(dataCh)

	if err := scanner.Err(); err != nil {
		m.log.Error("input error", "error", err)
	}

	<-handlerDone
	if m.settings.LiveMetrics {
		fmt.Fprint(os.Stderr, "\n") // Newline after the live stats line
	}
	m.log.Info("execution complete")
	return nil
}

func (m *Manager) processItem(ctx context.Context, wg *sync.WaitGroup, id int, inCh <-chan string, outCh chan<- string) {
	defer wg.Done()

	for {
		var data string
		var ok bool

		// First, wait to receive an item OR be cancelled.
		select {
		case <-ctx.Done():
			return
		case data, ok = <-inCh:
			if !ok {
				return // Channel is closed and empty.
			}
		}

		// Second, process the item we received.
		m.processedCount.Add(1)
		metricInput.Inc()

		normalized := normalizeInput(data)
		if normalized == "" {
			continue
		}

		item, err := publicsuffix.EffectiveTLDPlusOne(normalized)
		if err != nil || len(item) < 4 {
			metricOutput.WithLabelValues("invalid").Inc()
			continue
		}

		if m.settings.EnableRemote {
			if !m.checkRemote(ctx, item) {
				m.failedCount.Add(1)
				metricOutput.WithLabelValues("unverified").Inc()
				continue
			}
		}
		m.validCount.Add(1)
		metricOutput.WithLabelValues("verified").Inc()

		// Finally, try to send the result OR be cancelled if blocked.
		select {
		case outCh <- strings.ToLower(item):
			// Item sent successfully.
		case <-ctx.Done():
			// Was cancelled while waiting to send. Exit.
			return
		}
	}
}
func (m *Manager) resultHandler(ctx context.Context, inCh <-chan string, done chan<- struct{}) {
	defer close(done)
	seen := make(map[string]struct{})
	for item := range inCh {
		select {
		case <-ctx.Done():
			return
		default:
			if _, exists := seen[item]; !exists {
				seen[item] = struct{}{}
				fmt.Print(item)
				fmt.Print(m.settings.OutputDelim)
			}
		}
	}
}

func (m *Manager) checkRemote(ctx context.Context, target string) bool {
	reqURL := fmt.Sprintf("https://cloudflare-dns.com/dns-query?name=%s&type=A", url.QueryEscape(target))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false
	}
	req.Header.Add("Accept", "application/dns-json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var apiResp struct{ Status int }
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return false
	}
	return apiResp.Status == 0 // NOERROR
}

func (m *Manager) statsPrinter(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processed := m.processedCount.Load()
			valid := m.validCount.Load()
			failed := m.failedCount.Load()
			fmt.Fprintf(os.Stderr, "\rProcessed: %-10d | Valid: %-10d | Unverified: %-10d", processed, valid, failed)
		}
	}
}

func (m *Manager) getSource(path string) (io.ReadCloser, error) {
	if path != "" && path != "-" {
		return os.Open(path)
	}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return os.Stdin, nil
	}
	return nil, errors.New("no source specified and no data from pipe")
}

func normalizeInput(data string) string {
	data = strings.TrimSpace(data)
	if strings.Contains(data, "://") {
		if u, err := url.Parse(data); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	if host, _, err := net.SplitHostPort(data); err == nil {
		return host
	}
	return data
}

func main() {
	workerCount := flag.Int("w", runtime.NumCPU(), "Worker count.")
	delim := flag.String("d", "\n", "Output delimiter.")
	remoteCheck := flag.Bool("c", false, "Enable remote check for targets.")
	logLevel := flag.String("l", "info", "Log level (debug, info, warn, error).")
	liveMetrics := flag.Bool("live", false, "Display live metrics on stderr.")
	metricsAddr := flag.String("m-addr", "", "Enable Prometheus endpoint, e.g., ':9090'.")
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
	}

	app := NewManager(cfg)
	if err := app.Execute(context.Background()); err != nil {
		slog.Error("runtime error", "error", err)
		os.Exit(1)
	}
}
