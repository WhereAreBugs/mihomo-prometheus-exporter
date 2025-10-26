// collector.go
package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// MihomoCollector 实现了 prometheus.Collector 接口
type MihomoCollector struct {
	client *MihomoClient
	prefix string
	mutex  sync.RWMutex

	// 指标定义
	up                 *prometheus.Desc
	down               *prometheus.Desc
	activeConnections  *prometheus.Desc
	connectionUpload   *prometheus.Desc
	connectionDownload *prometheus.Desc
	proxyLatency       *prometheus.Desc
	proxyAvailable     *prometheus.Desc

	// 缓存从 API 获取的数据
	cachedTraffic        *Traffic
	cachedConnections    *ConnectionsResponse
	cachedProxyLatencies map[string]int
}

// NewMihomoCollector 创建并初始化一个 Collector
func NewMihomoCollector(client *MihomoClient, prefix string) *MihomoCollector {
	fqName := func(name string) string {
		return prometheus.BuildFQName(prefix, "", name)
	}
	return &MihomoCollector{
		client: client,
		prefix: prefix,
		up: prometheus.NewDesc(
			fqName("traffic_upload_speed_bytes"),
			"Current upload speed in bytes per second.",
			nil, nil,
		),
		down: prometheus.NewDesc(
			fqName("traffic_download_speed_bytes"),
			"Current download speed in bytes per second.",
			nil, nil,
		),
		activeConnections: prometheus.NewDesc(
			fqName("connections_active_total"),
			"Total number of active connections.",
			nil, nil,
		),
		connectionUpload: prometheus.NewDesc(
			fqName("connection_upload_bytes"),
			"Uploaded bytes for a specific active connection.",
			[]string{"source_host", "destination", "outbound_node"}, nil,
		),
		connectionDownload: prometheus.NewDesc(
			fqName("connection_download_bytes"),
			"Downloaded bytes for a specific active connection.",
			[]string{"source_host", "destination", "outbound_node"}, nil,
		),
		proxyLatency: prometheus.NewDesc(
			fqName("proxy_latency_ms"),
			"Latency of a specific proxy in milliseconds.",
			[]string{"proxy_name"}, nil,
		),
		proxyAvailable: prometheus.NewDesc(
			fqName("proxy_available"),
			"Availability of a specific proxy (1 for available, 0 for unavailable).",
			[]string{"proxy_name"}, nil,
		),
		cachedProxyLatencies: make(map[string]int),
	}
}

// Describe 将所有指标描述符发送到 channel
func (c *MihomoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.down
	ch <- c.activeConnections
	ch <- c.connectionUpload
	ch <- c.connectionDownload
	ch <- c.proxyLatency
	ch <- c.proxyAvailable
}

// Collect 从缓存中读取数据并生成指标，发送到 channel
func (c *MihomoCollector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.cachedTraffic != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, float64(c.cachedTraffic.Up))
		ch <- prometheus.MustNewConstMetric(c.down, prometheus.GaugeValue, float64(c.cachedTraffic.Down))
	}

	if c.cachedConnections != nil {
		ch <- prometheus.MustNewConstMetric(c.activeConnections, prometheus.GaugeValue, float64(len(c.cachedConnections.Connections)))

		// 定义用于聚合的 map 的 key 和 value 结构
		type connKey struct {
			sourceHost   string
			destination  string
			outboundNode string
		}
		type connTraffic struct {
			upload   int64
			download int64
		}
		// 创建聚合 map
		aggregatedConnections := make(map[connKey]connTraffic)

		for _, conn := range c.cachedConnections.Connections {
			outboundNode := "DIRECT" // 默认为直连
			if len(conn.Chains) > 0 {
				outboundNode = conn.Chains[len(conn.Chains)-1]
			}

			destination := conn.Metadata.Host
			if destination == "" {
				destination = conn.Metadata.DestinationIP
			}

			sourceHost := conn.Metadata.SourceIP

			// --- 2. 校验标签值，防止因空标签导致 panic ---
			if sourceHost == "" || destination == "" || outboundNode == "" {
				log.Printf("[WARN] Skipping connection with empty labels. ID: %s, Source: '%s', Destination: '%s', Node: '%s'",
					conn.ID, sourceHost, destination, outboundNode)
				continue // 跳过此连接
			}

			// --- 3. 聚合数据 ---
			key := connKey{sourceHost: sourceHost, destination: destination, outboundNode: outboundNode}
			traffic := aggregatedConnections[key]
			traffic.upload += conn.Upload
			traffic.download += conn.Download
			aggregatedConnections[key] = traffic
			// log.Printf("[DEBUG] Aggregated connection: key={src:%s, dst:%s, node:%s}, upload=%d, download=%d", key.sourceHost, key.destination, key.outboundNode, traffic.upload, traffic.download)
		}

		// 从聚合后的 map 生成指标
		for key, traffic := range aggregatedConnections {
			ch <- prometheus.MustNewConstMetric(c.connectionUpload, prometheus.GaugeValue, float64(traffic.upload), key.sourceHost, key.destination, key.outboundNode)
			ch <- prometheus.MustNewConstMetric(c.connectionDownload, prometheus.GaugeValue, float64(traffic.download), key.sourceHost, key.destination, key.outboundNode)
		}
	}
	if c.cachedProxyLatencies != nil {
		for name, delay := range c.cachedProxyLatencies {
			available := 1.0
			if delay <= 0 { // 延迟为0或负数通常表示超时或不可用
				available = 0.0
			}
			ch <- prometheus.MustNewConstMetric(c.proxyLatency, prometheus.GaugeValue, float64(delay), name)
			ch <- prometheus.MustNewConstMetric(c.proxyAvailable, prometheus.GaugeValue, available, name)
		}
	}
}

// updateFastMetrics 负责更新变化较快的指标（流量、连接）
func (c *MihomoCollector) updateFastMetrics(ctx context.Context) {
	//log.Println("Updating fast metrics (traffic, connections)...")

	var wg sync.WaitGroup
	var traffic *Traffic
	var connections *ConnectionsResponse
	//var proxies *ProxiesResponse
	var err error

	// 并发获取流量和连接信息
	wg.Add(2)
	go func() {
		defer wg.Done()
		traffic, err = c.client.GetTraffic(ctx)
		if err != nil {
			log.Printf("Error getting traffic: %v", err)
			return
		}
		c.mutex.Lock()
		c.cachedTraffic = traffic
		c.mutex.Unlock()
	}()
	go func() {
		defer wg.Done()
		connections, err = c.client.GetConnections(ctx)
		if err != nil {
			log.Printf("Error getting connections: %v", err)
			return
		}
		c.mutex.Lock()
		c.cachedConnections = connections
		c.mutex.Unlock()
	}()
	wg.Wait()
}

// updateSlowMetrics 负责更新变化较慢且耗时的指标（代理延迟）
func (c *MihomoCollector) updateSlowMetrics(ctx context.Context) {
	//log.Println("Updating slow metrics (proxy latency)...")

	var proxies *ProxiesResponse
	var err error
	latencies := make(map[string]int)

	// 获取代理列表，然后并发测试延迟
	proxies, err = c.client.GetProxies(ctx)
	if err != nil {
		log.Printf("Error getting proxies: %v", err)
	} else {
		var latencyWg sync.WaitGroup
		var latencyMutex sync.Mutex

		for name, p := range proxies.Proxies {
			// 只测试可用的代理节点，排除选择器、DIRECT等
			if p.Type == "Selector" || p.Type == "URLTest" || p.Type == "Fallback" || p.Type == "LoadBalance" || p.Type == "Direct" || p.Type == "Reject" {
				continue
			}
			latencyWg.Add(1)
			go func(proxyName string) {
				defer latencyWg.Done()
				delayInfo, err := c.client.GetProxyDelay(ctx, proxyName)
				if err != nil {
					//log.Printf("Error getting delay for proxy %s: %v", proxyName, err)
					latencyMutex.Lock()
					latencies[proxyName] = -1
					latencyMutex.Unlock()
					return
				}
				latencyMutex.Lock()
				latencies[proxyName] = delayInfo.Delay
				latencyMutex.Unlock()
			}(name)
		}
		latencyWg.Wait()
	}

	c.mutex.Lock()
	c.cachedProxyLatencies = latencies
	c.mutex.Unlock()
	//log.Println("Proxy latency metrics updated.")
}

// StartMonitors 启动两个后台循环，分别以不同的间隔更新指标
func (c *MihomoCollector) StartMonitors(ctx context.Context, fastInterval, slowInterval time.Duration) {
	// 启动快速监控循环 (流量, 连接)
	go func() {
		ticker := time.NewTicker(fastInterval)
		defer ticker.Stop()
		// 立即执行一次
		c.updateFastMetrics(ctx)
		for {
			select {
			case <-ticker.C:
				c.updateFastMetrics(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	// 启动慢速监控循环 (代理延迟)
	go func() {
		ticker := time.NewTicker(slowInterval)
		defer ticker.Stop()
		// 立即执行一次
		c.updateSlowMetrics(ctx)
		for {
			select {
			case <-ticker.C:
				c.updateSlowMetrics(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}
