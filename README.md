# Mihomo Prometheus Exporter

[![Go Version](https://img.shields.io/badge/go-1.18%2B-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

`mihomo-prometheus-exporter` 是一个轻量级、高性能的 Prometheus
Exporter，用于从 [Mihomo](https://github.com/MetaCubeX/mihomo) 中导出详细的运行时指标。

它被设计为异步运行，定期从 Mihomo API 拉取数据并缓存。这意味着 Prometheus 的抓取请求会立即得到响应，不会对 Mihomo
核心造成性能压力，确保了监控的稳定性和低延迟。

## 特性

* **异步抓取 (Asynchronous Scraping)**: 在后台独立地从 Mihomo API 获取数据，与 Prometheus 的抓取周期解耦，避免了在抓取时进行耗时的
  API 调用。
* **详尽的指标 (Detailed Metrics)**: 不仅仅是总流量，我们导出：
    * 实时的上传/下载速率。
    * 每个独立连接的流量、源 IP、目标域名/IP。
    * 每个连接所使用的**出站节点** (包括 `DIRECT`)。
    * 每个代理节点的延迟和可用性。

## 快速开始

### 前提条件

* Go 1.18 或更高版本。
* 一个正在运行的 Mihomo 实例，并已启用 `external-controller`。

### 安装与编译

1. 克隆或下载本项目代码。
2. 在项目根目录下，构建二进制文件：

   ```bash
   go build -o mihomo-exporter .
   ```

### 运行 Exporter

执行以下命令启动 Exporter。请务必将 API 地址和密钥替换为你的配置。

```bash
./mihomo-exporter \
  --web.listen-address=":9188" \
  --mihomo.api-url="http://127.0.0.1:9090" \
  --mihomo.api-token="YOUR_SECRET_TOKEN" \
  --scrape.interval="1s"
```

## 使用方法

### 命令行参数

| 参数                   | 环境变量 (`-` 替换为 `_`)   | 默认值                     | 描述                                     |
|----------------------|----------------------|-------------------------|----------------------------------------|
| `web.listen-address` | `WEB_LISTEN_ADDRESS` | `:9188`                 | Exporter 监听的地址和端口。                     |
| `mihomo.api-url`     | `MIHOMO_API_URL`     | `http://127.0.0.1:9097` | Mihomo `external-controller` 的 API 地址。 |
| `mihomo.api-token`   | `MIHOMO_API_TOKEN`   | `""`                    | Mihomo API 的 `secret` (如果设置了)。         |
| `scrape.interval`    | `SCRAPE_INTERVAL`    | `1s`                    | 从 Mihomo API 拉取数据的频率。                  |
| `latency.interval`   | `LATENCY_INTERVAL`   | `60s`                   | 从 Mihomo 进行统一延迟测试的频率                   |
| `metric.prefix`      | `METRIC_PREFIX`      | `mihomo`                | 导出的指标前缀                                |

### Prometheus 配置

将以下内容添加到你的 `prometheus.yml` 文件中，以开始抓取 Exporter 暴露的指标。

```yaml
scrape_configs:
  - job_name: 'mihomo'
    scrape_interval: 15s
    static_configs:
      - targets: [ 'localhost:9188' ] # 替换为 exporter 运行的地址
```

## 📈 导出的指标

以下是本 Exporter 提供的核心指标列表。

| 指标名称 | 类型 | 标签 (`l                            abel`)                                  | 描述 |
| ------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| `mihomo_traffic _upload_speed_bytes`       | Gauge | `(no ne)`                                        |
当前全局上传速率 (字节/秒)。 |
| `mihomo_traffic _download_speed_bytes`     | Gauge | `(no ne)`                                        |
当前全局下载速率 (字节/秒)。 |
| `mihomo_connect ions_active_total`         | Gauge | `(no ne)`                                        |
当前活跃连接的总数。 |
| `mihomo_connect ion_upload_bytes_total`    | Gauge | `sou rce_host`, `destination`, `outbound_node`   |
单个连接累计上传的字节数。 |
| `mihomo_connect ion_download_bytes_total`  | Gauge | `sou rce_host`, `destination`, `outbound_node`   |
单个连接累计下载的字节数。 |
| `mihomo_proxy_l atency_ms`                 | Gauge | `pro xy_name`                                    |
代理节点的延迟 (毫秒)。-1 表示测试失败。 |
| `mihomo_proxy_a vailable`                  | Gauge | `pro xy_name`                                    |
代理节点的可用性 (1=可用, 0=不可用)。 |

## PromQL 查询示例 (Grafana 看板灵感)

利用这些指标，你可以构建强大的仪表盘。

**1. 按出站节点统计的实时总流量速率 (Top 10)**

```promql
topk(10, sum(rate(mihomo_connection_download_bytes_total[1m]) + rate(mihomo_connection_upload_bytes_total[1m])) by (outbound_node))
```

**2. 按目标域名统计的下载流量 (Top 10)**

```promql
topk(10, sum(rate(mihomo_connection_download_bytes_total[5m])) by (destination))
```

**3. 各代理节点的平均延迟 (只显示可用节点)**

```promql
avg_over_time(mihomo_proxy_latency_ms{mihomo_proxy_available="1"}[5m])
```

**4. 统计每个出站节点的活跃连接数**

```promql
count(mihomo_connection_download_bytes_total) by (outbound_node)
```

## 贡献

欢迎提交 Pull Requests 或 Issues 来改进这个项目。

## 许可证

本项目基于 [MIT License](./LICENSE) 开源。