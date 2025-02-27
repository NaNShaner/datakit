---
title     : 'Log Streaming'
summary   : '通过 HTTP 上报日志数据'
icon      : 'icon/logstreaming'
dashboard :
  - desc  : '暂无'
    path  : '-'
monitor   :
  - desc  : '暂无'
    path  : '-'
---

<!-- markdownlint-disable MD025 -->
# Log Streaming
<!-- markdownlint-enable -->

---

{{.AvailableArchs}}

---

启动一个 HTTP Server，接收日志文本数据，上报到观测云。HTTP URL 固定为：`/v1/write/logstreaming`，即 `http://Datakit_IP:PORT/v1/write/logstreaming`

> 注：如果 DataKit 以 DaemonSet 方式部署在 Kubernetes 中，可以使用 Service 方式访问，地址为 `http://datakit-service.datakit:9529`

## 配置 {#config}

<!-- markdownlint-disable MD046 -->
=== "主机安装"

    进入 DataKit 安装目录下的 `conf.d/{{.Catalog}}` 目录，复制 `{{.InputName}}.conf.sample` 并命名为 `{{.InputName}}.conf`。示例如下：
    
    ```toml
    {{ CodeBlock .InputSample 4 }}
    ```

    配置好后，[重启 DataKit](datakit-service-how-to.md#manage-service) 即可。

=== "Kubernetes"

    目前可以通过 [ConfigMap 方式注入采集器配置](datakit-daemonset-deploy.md#configmap-setting)来开启采集器。
<!-- markdownlint-enable -->

### 支持参数 {#args}

Log-Streaming 支持在 HTTP URL 中添加参数，对日志数据进行操作。参数列表如下：

- `type`：数据格式，目前只支持 `influxdb`。
    - 当 `type` 为 `inflxudb` 时（`/v1/write/logstreaming?type=influxdb`），说明数据本身就是行协议格式（默认 precision 是 `s`），将只添加内置 Tags，不再做其他操作
    - 当此值为空时，会对数据做分行和 Pipeline 等处理
- `source`：标识数据来源，即行协议的 measurement。例如 `nginx` 或者 `redis`（`/v1/write/logstreaming?source=nginx`）
    - 当 `type` 是 `influxdb` 时，此值无效
    - 默认为 `default`
- `service`：添加 service 标签字段，例如（`/v1/write/logstreaming?service=nginx_service`）
    - 默认为 `source` 参数值。
- `pipeline`：指定数据需要使用的 pipeline 名称，例如 `nginx.p`（`/v1/write/logstreaming?pipeline=nginx.p`）
- `tags`：添加自定义 tag，以英文逗号 `,` 分割，例如 `key1=value1` 和 `key2=value2`（`/v1/write/logstreaming?tags=key1=value1,key2=value2`）

### 使用方式 {#usage}

- Fluentd 使用 Influxdb Output [文档](https://github.com/fangli/fluent-plugin-influxdb){:target="_blank"}
- Fluentd 使用 HTTP Output [文档](https://docs.fluentd.org/output/http){:target="_blank"}
- Logstash 使用 Influxdb Output [文档](https://www.elastic.co/guide/en/logstash/current/plugins-outputs-influxdb.html){:target="_blank"}
- Logstash 使用 HTTP Output [文档](https://www.elastic.co/guide/en/logstash/current/plugins-outputs-http.html){:target="_blank"}

只需要将 Output Host 配置为 Log-Streaming URL（`http://Datakit_IP:PORT/v1/write/logstreaming`）并添加对应参数即可。

## 日志 {#logging}

{{ range $i, $m := .Measurements }}

### `{{$m.Name}}`

{{$m.Desc}}

- 标签

{{$m.TagsMarkdownTable}}

- 指标列表

{{$m.FieldsMarkdownTable}}

{{ end }}
