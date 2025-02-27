
# RabbitMQ
---

{{.AvailableArchs}}

---

RabbitMQ collector monitors RabbitMQ by collecting data through the plug-in `rabbitmq-management` and can:

- RabbitMQ overview, such as connections, queues, total messages, and so on.
- Track RabbitMQ queue information, such as queue size, consumer count and so on.
- Rack RabbitMQ node information, such as `socket` `mem`.
- Tracking RabbitMQ exchange information such as `message_publish_count`.

## Preconditions {#reqirement}

- RabbitMQ version >= `3.8.14`; Already tested version:
    - [x] 3.11.x
    - [x] 3.10.x
    - [x] 3.9.x
    - [x] 3.8.x

- Install `rabbitmq`, take `Ubuntu` as an example

    ```shell
    sudo apt-get update
    sudo apt-get install rabbitmq-server
    sudo service rabbitmq-server start
    ```

- Start `REST API plug-ins`

    ```shell
    sudo rabbitmq-plugins enable rabbitmq-management
    ```

- Creat user, for example:

    ```shell
    sudo rabbitmqctl add_user guance <SECRET>
    sudo rabbitmqctl set_permissions  -p / guance "^aliveness-test$" "^amq\.default$" ".*"
    sudo rabbitmqctl set_user_tags guance monitoring
    ```

## Cnfiguration {#config}

=== "Host Installation"

    Go to the `conf.d/{{.Catalog}}` directory under the DataKit installation directory, copy `{{.InputName}}.conf.sample` and name it `{{.InputName}}.conf`. Examples are as follows:


​    
    ```toml
    {{ CodeBlock .InputSample 4 }}
    ```
    
    After configuration, [restart DataKit](datakit-service-how-to.md#manage-service).

=== "Kubernetes"

    The collector can now be turned on by [ConfigMap injection collector configuration](datakit-daemonset-deploy.md#configmap-setting).

## Measurements {#measurements}

For all of the following data collections, a global tag named `host` is appended by default (the tag value is the host name of the DataKit), or other tags can be specified in the configuration by `[inputs.rabbitmq.tags]`:

``` toml
 [inputs.rabbitmq.tags]
  # some_tag = "some_value"
  # more_tag = "some_other_value"
  # ...
```

{{ range $i, $m := .Measurements }}

### `{{$m.Name}}`

- tag

{{$m.TagsMarkdownTable}}

- metric list

{{$m.FieldsMarkdownTable}}

{{ end }}


## Log Collection {#logging}

???+ attention

    DataKit must be installed on the host where RabbitMQ is located to collect RabbitMQ logs.

To collect the RabbitMQ log, open `files` in RabbitMQ.conf and write to the absolute path of the RabbitMQ log file. For example:

```toml
    [[inputs.rabbitmq]]
      ...
      [inputs.rabbitmq.log]
        files = ["/var/log/rabbitmq/rabbit@your-hostname.log"]
```


When log collection is turned on, a log with a log `source` of `rabbitmq` is generated by default.

## Log Pipeline Function Cut Field Description {#pipeline}

- RabbitMQ universal log cutting

Example of common log text:

```
2021-05-26 14:20:06.105 [warning] <0.12897.46> rabbitmqctl node_health_check and its HTTP API counterpart are DEPRECATED. See https://www.rabbitmq.com/monitoring.html#health-checks for replacement options.
```

The list of cut fields is as follows:

| Field Name | Field Value                             | Description                         |
| ---    | ---                                | ---                          |
| status | warning                            | Log level                     |
| msg    | <0.12897.46>...replacement options | Log level                     |
| time   | 1622010006000000000                | Nanosecond timestamp (as row protocol time) |
