package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/weppos/publicsuffix-go/publicsuffix"
)

func main() {
	// CLI flags
	workers := flag.Int("workers", runtime.NumCPU(), "number of concurrent parser workers")
	bufSize := flag.Int("bufsize", 1024*1024, "scanner buffer size in bytes")
	sep := flag.String("sep", "\n", "output separator (default newline)")
	ignorePrivate := flag.Bool("ignore-private", true, "ignore private TLD rules")
	flag.Parse()

	// Halt if no file arg AND nothing is piped in (stdin is a TTY)
	if flag.NArg() == 0 {
		if fi, err := os.Stdin.Stat(); err == nil {
			if (fi.Mode() & os.ModeCharDevice) != 0 {
				log.Println("No input detected. Provide a file path or pipe data in.")
				flag.Usage()
				os.Exit(1)
			}
		}
	}

	// Input: file arg or stdin
	in := os.Stdin
	if flag.NArg() > 0 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			log.Fatalf("unable to open %s: %v", flag.Arg(0), err)
		}
		defer f.Close()
		in = f
	}

	// Scanner
	scanner := bufio.NewScanner(in)
	buf := make([]byte, *bufSize)
	scanner.Buffer(buf, *bufSize)

	jobs := make(chan string, 1000)
	results := make(chan string, 1000)

	// Workers
	var wg sync.WaitGroup
	wg.Add(*workers)

	for i := 0; i < *workers; i++ {
		go func() {
			defer wg.Done()
			for line := range jobs {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				host := extractHost(line)
				if host == "" {
					continue
				}

				domain, err := publicsuffix.DomainFromListWithOptions(
					publicsuffix.DefaultList, // <-- variable, not function
					host,
					&publicsuffix.FindOptions{IgnorePrivate: *ignorePrivate},
				)
				if err != nil || domain == "" {
					continue // skip invalid/unknown hosts
				}
				results <- strings.ToLower(domain)
			}
		}()
	}

	// Close results when workers finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Aggregator/deduplicator
	done := make(chan struct{})
	go func() {
		seen := make(map[string]struct{})
		for domain := range results {
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			fmt.Print(domain)
			fmt.Print(*sep)
		}
		close(done)
	}()

	// Feed lines
	for scanner.Scan() {
		jobs <- scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		log.Printf("scanner error: %v", err)
	}
	close(jobs)

	<-done // wait for aggregator
}

// extractHost normalises the input string and returns just the hostname.
// It accepts raw hostnames, full URLs, and host:port combos.
func extractHost(input string) string {
	if strings.Contains(input, "://") {
		if u, err := url.Parse(input); err == nil {
			return u.Hostname()
		}
	}

	input = strings.TrimSpace(input)
	if idx := strings.IndexAny(input, " \t"); idx != -1 {
		input = input[:idx]
	}

	if host, _, err := net.SplitHostPort(input); err == nil {
		return host
	}

	return input
}
