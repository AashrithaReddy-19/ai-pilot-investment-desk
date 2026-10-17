// Command bench is a small, honest latency benchmark against a RUNNING API (not production-scale validation).
//
//	go run ./cmd/bench -url http://127.0.0.1:8080 -label "redis on, ai online" -out reports/benchmark.json
//
// Workloads (each: N requests, all fired concurrently):
//
//	evaluate   POST /api/v1/pilots/{AI1..AI5}/evaluate   (DB read + engine + decision insert + AI explain)
//	portfolio  GET  /api/v1/portfolios/{id}              (DB reads + assessment)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type result struct {
	Label     string         `json:"label"`
	Workload  string         `json:"workload"`
	Requests  int            `json:"requests"`
	Conc      int            `json:"concurrency"`
	P50ms     float64        `json:"p50_ms"`
	P95ms     float64        `json:"p95_ms"`
	P99ms     float64        `json:"p99_ms"`
	MaxMs     float64        `json:"max_ms"`
	ErrorRate float64        `json:"error_rate"`
	AIStatus  map[string]int `json:"ai_status,omitempty"`
	Cache     map[string]int `json:"cache,omitempty"`
}

func main() {
	url := flag.String("url", "http://127.0.0.1:8080", "API base URL")
	n := flag.Int("n", 100, "requests per workload (all concurrent)")
	label := flag.String("label", "", "configuration label")
	out := flag.String("out", "", "append results to this JSON file")
	inproc := flag.Bool("inprocess", false, "start embedded PostgreSQL and benchmark the 4 standard configurations over loopback HTTP")
	ai := flag.String("ai", "http://127.0.0.1:8001", "AI service URL used for the 'AI online' configurations (inprocess mode)")
	flag.Parse()
	if *inproc {
		runInProcess(*n, *ai, *out)
		return
	}
	var all []result
	all = append(all, benchURL(*url, *n, *label)...)
	save(*out, all)
}

func save(out string, res []result) {
	if out == "" {
		return
	}
	var all []result
	if b, err := os.ReadFile(out); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	all = append(all, res...)
	b, _ := json.MarshalIndent(all, "", "  ")
	_ = os.WriteFile(out, append(b, '\n'), 0o644)
}

func benchURL(url string, n int, label string) []result {
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 200}}
	pid := createPortfolio(client, url)
	ids := []string{"AI1", "AI2", "AI3", "AI4", "AI5"}
	res := []result{
		run(label, "evaluate", n, func(i int) (*http.Response, error) {
			return client.Post(url+"/api/v1/pilots/"+ids[i%5]+"/evaluate", "application/json", nil)
		}),
		run(label, "portfolio", n, func(int) (*http.Response, error) { return client.Get(url + "/api/v1/portfolios/" + pid) }),
	}
	for _, r := range res {
		fmt.Printf("%-28s %-9s n=%d p50=%.1fms p95=%.1fms p99=%.1fms max=%.1fms errors=%.1f%% ai=%v cache=%v\n",
			r.Label, r.Workload, r.Requests, r.P50ms, r.P95ms, r.P99ms, r.MaxMs, r.ErrorRate*100, r.AIStatus, r.Cache)
	}
	return res
}

func createPortfolio(c *http.Client, url string) string {
	body := fmt.Sprintf(`{"name":"bench %d","budget_inr":600000,"pilot_ids":["AI1","AI2","AI4"]}`, time.Now().UnixNano())
	r, err := c.Post(url+"/api/v1/portfolios", "application/json", bytes.NewBufferString(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot reach API:", err)
		os.Exit(1)
	}
	defer r.Body.Close()
	var p struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)
	if p.ID == "" {
		fmt.Fprintln(os.Stderr, "portfolio create failed:", r.Status)
		os.Exit(1)
	}
	return p.ID
}

func run(label, name string, n int, do func(int) (*http.Response, error)) result {
	lat := make([]float64, n)
	errs := 0
	ai, cache := map[string]int{}, map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			t := time.Now()
			r, err := do(i)
			d := float64(time.Since(t).Microseconds()) / 1000
			mu.Lock()
			defer mu.Unlock()
			lat[i] = d
			if err != nil {
				errs++
				return
			}
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode >= 400 {
				errs++
			}
			var m struct {
				AI string `json:"ai_status"`
			}
			if json.Unmarshal(b, &m) == nil && m.AI != "" {
				ai[m.AI]++
			}
			if h := r.Header.Get("X-Cache"); h != "" {
				cache[h]++
			}
		}(i)
	}
	close(start)
	wg.Wait()
	sort.Float64s(lat)
	pct := func(p float64) float64 { return lat[int(p*float64(n-1))] }
	return result{Label: label, Workload: name, Requests: n, Conc: n, P50ms: pct(0.50), P95ms: pct(0.95), P99ms: pct(0.99), MaxMs: lat[n-1],
		ErrorRate: float64(errs) / float64(n), AIStatus: ai, Cache: cache}
}
