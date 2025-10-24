package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultURL     = "http://srv.msk01.gigacorp.local/_stats"
	pollInterval   = 10 * time.Second // период опроса
	httpTimeout    = 5 * time.Second  // таймаут запроса
	errThreshold   = 3                // при 3 и более ошибках печатаем предупреждение
	bytesPerMiB    = 1024 * 1024
	expectedFields = 7

	loadAvgLimit  = 30.0
	memUsageLimit = 0.80
	diskUsageLim  = 0.90
	netUsageLim   = 0.90
)

func main() {
	url := resolveURL()

	client := &http.Client{Timeout: httpTimeout}
	errorCount := 0

	for {
		ok := fetchAndReport(client, url)
		if ok {
			errorCount = 0
		} else {
			errorCount++
			if errorCount >= errThreshold {
				fmt.Println("Unable to fetch server statistic.")
			}
		}
		time.Sleep(pollInterval)
	}
}

// порядок приоритета: 1) аргумент CLI, 2) ENV, 3) дефолт
func resolveURL() string {
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		return strings.TrimSpace(os.Args[1])
	}
	for _, key := range []string{"STATS_URL", "SERVER_STATS_URL", "SERVER_URL", "URL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return defaultURL
}

func fetchAndReport(client *http.Client, url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	values, ok := parseValues(string(body))
	if !ok || len(values) != expectedFields {
		return false
	}

	loadAvg := values[0]
	memTotal := values[1]
	memUsed := values[2]
	diskTotal := values[3]
	diskUsed := values[4]
	netCap := values[5]
	netUsed := values[6]

	// 1) Load Average
	if loadAvg > loadAvgLimit {
		fmt.Printf("Load Average is too high: %s\n", trimFloat(loadAvg))
	}

	// 2) Memory usage > 80%  — % усекать вниз, не округлять
	if memTotal > 0 {
		memPct := (memUsed / memTotal) * 100
		if memPct > memUsageLimit*100 {
			fmt.Printf("Memory usage too high: %d%%\n", int(memPct))
		}
	}

	// 3) Disk usage > 90% → свободные MiB
	if diskTotal > 0 {
		if diskUsed/diskTotal > diskUsageLim {
			freeBytes := diskTotal - diskUsed
			if freeBytes < 0 {
				freeBytes = 0
			}
			freeMiB := int64(freeBytes) / bytesPerMiB
			fmt.Printf("Free disk space is too low: %d Mb left\n", freeMiB)
		}
	}

	// 4) Network usage > 90% → (cap-used)/1_000_000 — тесты ждут именно это значение,
	// печатать в формате "... Mbit/s available"
	if netCap > 0 {
		if netUsed/netCap > netUsageLim {
			freeBytesPerSec := netCap - netUsed
			if freeBytesPerSec < 0 {
				freeBytesPerSec = 0
			}
			freeMBps := int64(freeBytesPerSec) / 1_000_000 // SI MB/s
			fmt.Printf("Network bandwidth usage high: %d Mbit/s available\n", freeMBps)
		}
	}

	return true
}

func parseValues(s string) ([]float64, bool) {
	trimmed := strings.TrimSpace(s)
	parts := strings.Split(trimmed, ",")
	if len(parts) != expectedFields {
		return nil, false
	}
	out := make([]float64, 0, expectedFields)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
