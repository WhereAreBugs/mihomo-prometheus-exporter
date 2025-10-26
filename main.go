// main.go
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 命令行参数定义
	listenAddress := flag.String("web.listen-address", ":9188", "Address to listen on for web interface and telemetry.")
	mihomoAPI := flag.String("mihomo.api-url", "http://127.0.0.1:9097", "Mihomo API base URL.")
	mihomoToken := flag.String("mihomo.api-token", "", "Mihomo API secret token (if any).")
	scrapeInterval := flag.Duration("scrape.interval", 1*time.Second, "Interval at which to scrape Mihomo API.")
	latencyInterval := flag.Duration("latency.interval", 60*time.Second, "Interval at which to test proxy latency.")
	metricPrefix := flag.String("metric.prefix", "mihomo", "Prefix for all exported metrics.")
	flag.Parse()

	log.Println("Starting mihomo-prometheus-exporter...")
	log.Printf("Listening on %s", *listenAddress)
	log.Printf("Connecting to Mihomo API at %s", *mihomoAPI)

	// 创建 Mihomo 客户端
	client, err := NewMihomoClient(*mihomoAPI, *mihomoToken)
	if err != nil {
		log.Fatalf("Failed to create Mihomo client: %v", err)
	}

	// 创建并注册 Collector
	collector := NewMihomoCollector(client, *metricPrefix)
	prometheus.MustRegister(collector)

	// 创建一个带取消功能的 context 用于优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 在后台启动双轨异步更新器
	collector.StartMonitors(ctx, *scrapeInterval, *latencyInterval)

	// 设置 HTTP 服务器
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>
             <head><title>Mihomo Exporter</title></head>
             <body>
             <h1>Mihomo Exporter</h1>
             <p><a href='/metrics'>Metrics</a></p>
             </body>
             </html>`))
	})

	server := &http.Server{Addr: *listenAddress}

	// 优雅关闭
	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
		<-sigchan
		log.Println("Shutdown signal received, gracefully shutting down...")
		cancel() // 通知更新器停止
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("HTTP server shutdown failed: %v", err)
		}
	}()

	// 启动 HTTP 服务器
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server ListenAndServe: %v", err)
	}

	log.Println("Exporter stopped.")
}
