package main

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