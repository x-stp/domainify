# 🚀 domainify 

*The most ridiculously over-engineered domain processor you never knew you needed*

## What Does This Monster Do?

This Go application takes domains, URLs, or whatever vaguely domain-shaped text you throw at it and:
- ✨ Normalizes them into proper domain format
- 🔥 Processes them with blazing parallel workers
- 🌐 Optionally verifies they exist via DNS (because trust issues)
- 📊 Spits out Prometheus metrics (because observability is life)
- 🎯 Deduplicates everything (no repeats allowed)
- 💨 Does it all fast enough to make your CPU sweat

## Features That'll Blow Your Mind

- **Parallel Processing**: Uses all your CPU cores by default because why not?
- **DNS Verification**: Checks if domains actually exist using Cloudflare's DNS (fancy!)
- **Prometheus Metrics**: Track everything with professional-grade metrics
- **Live Stats**: Watch numbers go up in real-time on stderr
- **Smart Input Handling**: Accepts URLs, domains, host:port combos - it figures it out
- **Graceful Shutdown**: Handles SIGTERM like a gentleman
- **Memory Pool**: Reuses buffers because garbage collection is for peasants

## Installation

```bash
go build -o domainify
```

Or if you're feeling dangerous:
```bash
go run main.go
```

## Usage Examples

### Basic Usage (Boring but Works)
```bash
echo "example.com" | ./domain-dominator
```

### Feed It a File
```bash
./domain-dominator domains.txt
```

### Go Full Chaos Mode
```bash
./domain-dominator -w 50 -c -live -m-addr :9090 -d "," massive-domain-list.txt
```

## Command Line Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-w` | Worker count (more = faster, maybe) | Your CPU count |
| `-d` | Output delimiter | `\n` |
| `-c` | Enable DNS verification | `false` |
| `-l` | Log level (debug/info/warn/error) | `info` |
| `-live` | Show live stats on stderr | `false` |
| `-m-addr` | Prometheus metrics address | disabled |

## Input Formats Supported

This beast accepts:
- Plain domains: `example.com`
- URLs: `https://example.com/path`
- Host:port combos: `example.com:8080`
- Basically anything with a domain in it

## Metrics Endpoints

Hit `/metrics` on your configured address for Prometheus goodness:
- `op_input_total` - Items processed
- `op_output_total{status="verified|unverified|invalid"}` - Results by status

## Performance Notes

- Uses worker pools for maximum throughput
- Memory pools to avoid GC pressure
- Atomic counters for thread-safe stats
- Buffered channels sized for your worker count

## Why This Exists

Because sometimes you need to process millions of domains and regular tools just don't cut it. This tool was born from the ashes of slow Python scripts and the tears of sysadmins everywhere.

## Requirements

- Go 1.19+
- Internet connection (for DNS checks)
- Enough RAM to hold your dedupe map
- Patience (or lack thereof)

## License

MIT - Do whatever you want with it, but don't blame me if it processes your domains too efficiently.

---

*Built with ❤️ and excessive use of goroutines*
